package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"embed"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed views-net.js views-net.css
var netViewFiles embed.FS

var devtoolsPortRE = regexp.MustCompile(`(?i)DevTools listening on ws://(?:127\.0\.0\.1|localhost|\[::1\]):(\d+)/`)

func (a *app) netViewAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Use GET to load this view", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/")
	content, err := netViewFiles.ReadFile(name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	contentType := "text/javascript; charset=utf-8"
	if strings.HasSuffix(name, ".css") {
		contentType = "text/css; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodGet {
		_, _ = w.Write(content)
	}
}

type browserPage struct {
	ID                   string `json:"id"`
	Title                string `json:"title"`
	URL                  string `json:"url"`
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl,omitempty"`
}

type browserState struct {
	Running      bool          `json:"running"`
	PID          int           `json:"pid,omitempty"`
	Port         int           `json:"port,omitempty"`
	Memory       int64         `json:"memory,omitempty"`
	Headless     bool          `json:"headless,omitempty"`
	AgentDriving bool          `json:"agentDriving,omitempty"`
	Pages        []browserPage `json:"pages"`
	Error        string        `json:"error,omitempty"`
}

type browserOutput struct {
	mu   sync.Mutex
	data bytes.Buffer
	port chan int
	done bool
}

func (o *browserOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	_, _ = o.data.Write(p)
	if !o.done {
		if port, ok := parseBrowserPort(o.data.Bytes()); ok {
			o.done = true
			o.port <- port
		}
	}
	return len(p), nil
}

func parseBrowserPort(output []byte) (int, bool) {
	match := devtoolsPortRE.FindSubmatch(output)
	if len(match) != 2 {
		return 0, false
	}
	port, err := strconv.Atoi(string(match[1]))
	return port, err == nil && port > 0 && port <= 65535
}

type browserManager struct {
	mu          sync.Mutex
	home        string
	ctx         context.Context
	cmd         *exec.Cmd
	done        chan struct{}
	pid         int
	port        int
	headless    bool
	lastErr     string
	connections int
}

func newBrowserManager(home string, ctx context.Context) *browserManager {
	return &browserManager{home: home, ctx: ctx}
}

func browserCandidates(home string) []string {
	paths := []string{}
	for _, name := range []string{"google-chrome-stable", "google-chrome", "chromium", "chromium-browser", "chrome"} {
		if path, err := exec.LookPath(name); err == nil {
			paths = append(paths, path)
		}
	}
	paths = append(paths,
		filepath.Join(home, ".agent-browser", "chromium"),
		filepath.Join(home, ".agent-browser", "chrome"))
	for _, pattern := range []string{
		filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome"),
		filepath.Join(home, ".cache", "ms-playwright", "chromium-*", "chrome-linux", "chrome-headless-shell"),
		filepath.Join(home, ".agent-browser", "**", "chrome"),
	} {
		matches, _ := filepath.Glob(pattern)
		paths = append(paths, matches...)
	}
	result := []string{}
	seen := map[string]bool{}
	for _, path := range paths {
		if strings.ContainsAny(path, "*") {
			continue
		}
		st, err := os.Stat(path)
		if err != nil || st.IsDir() || st.Mode()&0111 == 0 || seen[path] {
			continue
		}
		seen[path] = true
		result = append(result, path)
	}
	return result
}

func (m *browserManager) runningLocked() bool {
	if m.cmd == nil || m.cmd.Process == nil || m.port == 0 {
		return false
	}
	return m.cmd.Process.Signal(syscall.Signal(0)) == nil
}

func (m *browserManager) start(headless bool) (browserState, error) {
	m.mu.Lock()
	if m.runningLocked() {
		state := m.stateLocked()
		m.mu.Unlock()
		return state, nil
	}
	m.cmd, m.done = nil, nil
	m.pid, m.port = 0, 0
	m.lastErr = ""
	m.mu.Unlock()

	candidates := browserCandidates(m.home)
	if len(candidates) == 0 {
		return browserState{}, fmt.Errorf("no Chromium or Chrome executable was found")
	}
	profile := filepath.Join(m.home, ".local", "share", "boxdeck", "browser")
	if err := os.MkdirAll(profile, 0700); err != nil {
		return browserState{}, fmt.Errorf("browser profile: %w", err)
	}
	args := []string{
		"--remote-debugging-port=0",
		"--remote-debugging-address=127.0.0.1",
		"--user-data-dir=" + profile,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-background-networking",
		"--disable-dev-shm-usage",
		"--remote-allow-origins=*",
		"--no-sandbox",
	}
	if headless {
		args = append(args, "--headless=new")
	}
	cmd := exec.Command(candidates[0], args...)
	output := &browserOutput{port: make(chan int, 1)}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		return browserState{}, fmt.Errorf("start browser: %w", err)
	}
	m.mu.Lock()
	done := make(chan struct{})
	m.cmd, m.done, m.pid, m.headless = cmd, done, cmd.Process.Pid, headless
	m.mu.Unlock()
	go func() {
		err := cmd.Wait()
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.cmd == cmd {
			m.lastErr = "browser stopped"
			if err != nil {
				m.lastErr = "browser stopped: " + err.Error()
			}
			m.cmd, m.done, m.pid, m.port = nil, nil, 0, 0
		}
		close(done)
	}()

	select {
	case port := <-output.port:
		m.mu.Lock()
		if m.cmd != cmd {
			m.mu.Unlock()
			return browserState{}, fmt.Errorf("browser stopped before it was ready")
		}
		m.port = port
		state := m.stateLocked()
		m.mu.Unlock()
		return state, nil
	case <-time.After(10 * time.Second):
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return browserState{}, fmt.Errorf("browser did not report a debugging port")
	case <-m.ctx.Done():
		_ = cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
		return browserState{}, fmt.Errorf("boxdeck is stopping")
	}
}

func (m *browserManager) stop() error {
	m.mu.Lock()
	cmd := m.cmd
	done := m.done
	m.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	if err := cmd.Process.Kill(); err != nil && !strings.Contains(err.Error(), "process already finished") {
		return err
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}
	return nil
}

func processMemory(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	if runtime.GOOS == "darwin" {
		return darwinProcessRSS(pid)
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			return int64(integer(fields[1])) * 1024
		}
	}
	return 0
}

func (m *browserManager) stateLocked() browserState {
	state := browserState{Pages: []browserPage{}}
	if !m.runningLocked() {
		state.Error = m.lastErr
		return state
	}
	state.Running, state.PID, state.Port, state.Memory, state.Headless = true, m.pid, m.port, processMemory(m.pid), m.headless
	state.AgentDriving = m.connections > 0
	return state
}

func (m *browserManager) status() browserState {
	m.mu.Lock()
	state := m.stateLocked()
	port := m.port
	m.mu.Unlock()
	if !state.Running || port == 0 {
		return state
	}
	pages, err := browserPages(context.Background(), port)
	if err != nil {
		state.Error = "Could not read browser pages"
	} else {
		state.Pages = pages
	}
	return state
}

func browserPages(ctx context.Context, port int) ([]browserPage, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, "http://127.0.0.1:"+strconv.Itoa(port)+"/json/list", nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("browser returned HTTP %d", res.StatusCode)
	}
	var pages []browserPage
	if err := json.NewDecoder(io.LimitReader(res.Body, 2<<20)).Decode(&pages); err != nil {
		return nil, err
	}
	return pages, nil
}

func (a *app) browserStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to start the browser"})
		return
	}
	var body struct {
		Headless *bool `json:"headless"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if !decodeJSONBody(w, r, &body, 4096) {
			return
		}
	}
	headless := true
	if body.Headless != nil {
		headless = *body.Headless
	}
	state, err := a.browser.start(headless)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	jsonReply(w, http.StatusOK, state)
}

func (a *app) browserStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to stop the browser"})
		return
	}
	if err := a.browser.stop(); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not stop the browser"})
		return
	}
	jsonReply(w, http.StatusOK, a.browser.status())
}

func (a *app) browserStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read browser state"})
		return
	}
	jsonReply(w, http.StatusOK, a.browser.status())
}

func (a *app) browserTabs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodDelete {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to open or DELETE to close a browser tab"})
		return
	}
	port := a.browserPort()
	if port == 0 {
		jsonReply(w, http.StatusConflict, object{"error": "Start the browser first"})
		return
	}
	path := "/json/new"
	method := http.MethodPut
	if r.Method == http.MethodDelete {
		id := strings.TrimSpace(r.URL.Query().Get("page"))
		if id == "" || strings.ContainsAny(id, "/\\?#\r\n") {
			jsonReply(w, http.StatusBadRequest, object{"error": "page is required"})
			return
		}
		path = "/json/close/" + url.PathEscape(id)
		method = http.MethodGet
	} else {
		var body struct {
			URL string `json:"url"`
		}
		if r.Body != nil && r.ContentLength != 0 && !decodeJSONBody(w, r, &body, 4096) {
			return
		}
		navigation := strings.TrimSpace(body.URL)
		if navigation == "" {
			navigation = "about:blank"
		}
		if navigation != "about:blank" {
			target, err := url.Parse(navigation)
			if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
				jsonReply(w, http.StatusBadRequest, object{"error": "url must be an http(s) URL"})
				return
			}
		}
		path += "?" + url.QueryEscape(navigation)
	}
	requestCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, method, "http://127.0.0.1:"+strconv.Itoa(port)+path, nil)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not reach the browser"})
		return
	}
	response, err := (&http.Client{Timeout: 3 * time.Second}).Do(request)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not reach the browser"})
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		jsonReply(w, http.StatusBadGateway, object{"error": "Browser tab action failed"})
		return
	}
	var page browserPage
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&page); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Browser returned an invalid tab"})
		return
	}
	jsonReply(w, http.StatusOK, page)
}

func browserPageFor(pages []browserPage, id string) (browserPage, bool) {
	for _, page := range pages {
		if page.ID == id {
			return page, true
		}
	}
	if id != "" {
		return browserPage{}, false
	}
	for _, page := range pages {
		if page.Type == "page" && page.WebSocketDebuggerURL != "" {
			return page, true
		}
	}
	return browserPage{}, false
}

func (a *app) browserShot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to capture a screenshot"})
		return
	}
	state := a.browser.status()
	if !state.Running {
		jsonReply(w, http.StatusConflict, object{"error": "Start the browser first"})
		return
	}
	page, ok := browserPageFor(state.Pages, r.URL.Query().Get("page"))
	if !ok {
		jsonReply(w, http.StatusNotFound, object{"error": "No browser page is open"})
		return
	}
	data, err := captureCDPScreenshot(r.Context(), page.WebSocketDebuggerURL)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not capture this page"})
		return
	}
	jsonReply(w, http.StatusOK, object{"page": page.ID, "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(data)})
}

func rewriteCDPURL(value, scheme, publicBase string) string {
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Path == "" {
		return value
	}
	publicScheme := "ws"
	if scheme == "https" {
		publicScheme = "wss"
	}
	base := strings.TrimRight(publicBase, "/")
	return publicScheme + "://" + base + u.EscapedPath()
}

func rewriteCDPValue(value any, scheme, publicBase string) {
	switch current := value.(type) {
	case map[string]any:
		for key, child := range current {
			if key == "webSocketDebuggerUrl" {
				if text, ok := child.(string); ok {
					current[key] = rewriteCDPURL(text, scheme, publicBase)
					continue
				}
			}
			rewriteCDPValue(child, scheme, publicBase)
		}
	case []any:
		for _, child := range current {
			rewriteCDPValue(child, scheme, publicBase)
		}
	}
}

func rewriteCDPJSON(body []byte, scheme, publicBase string) ([]byte, error) {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return nil, err
	}
	rewriteCDPValue(value, scheme, publicBase)
	return json.Marshal(value)
}

func cdpUpstreamPath(path string) string {
	trimmed := strings.TrimPrefix(path, "/cdp")
	if trimmed == "" {
		return "/"
	}
	if !strings.HasPrefix(trimmed, "/") {
		return "/" + trimmed
	}
	return trimmed
}

func cdpQueryWithoutToken(query url.Values) string {
	copyQuery := url.Values{}
	for key, values := range query {
		if key == "token" {
			continue
		}
		for _, value := range values {
			copyQuery.Add(key, value)
		}
	}
	return copyQuery.Encode()
}

func (a *app) cdp(w http.ResponseWriter, r *http.Request) {
	port := a.browserPort()
	if port == 0 {
		jsonReply(w, http.StatusConflict, object{"error": "Start the browser first"})
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		a.cdpWebsocket(w, r, port)
		return
	}
	a.cdpHTTP(w, r, port)
}

func (a *app) browserPort() int {
	a.browser.mu.Lock()
	defer a.browser.mu.Unlock()
	if !a.browser.runningLocked() {
		return 0
	}
	return a.browser.port
}

func (a *app) cdpHTTP(w http.ResponseWriter, r *http.Request, port int) {
	upstreamURL := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), Path: cdpUpstreamPath(r.URL.Path), RawQuery: cdpQueryWithoutToken(r.URL.Query())}
	requestCtx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, r.Method, upstreamURL.String(), r.Body)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not reach the browser"})
		return
	}
	for key, values := range r.Header {
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	request.Header.Del("Authorization")
	request.Header.Del("Cookie")
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not reach the browser"})
		return
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, 8<<20))
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not read the browser response"})
		return
	}
	if strings.HasPrefix(cdpUpstreamPath(r.URL.Path), "/json") && strings.Contains(response.Header.Get("Content-Type"), "json") {
		if rewritten, rewriteErr := rewriteCDPJSON(body, schemeForRequest(r), r.Host+"/cdp"); rewriteErr == nil {
			body = rewritten
		}
	}
	for key, values := range response.Header {
		if strings.EqualFold(key, "Content-Length") || strings.EqualFold(key, "Transfer-Encoding") {
			continue
		}
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(body)))
	w.WriteHeader(response.StatusCode)
	_, _ = w.Write(body)
}

func schemeForRequest(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func (a *app) cdpWebsocket(w http.ResponseWriter, r *http.Request, port int) {
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		http.Error(w, "Browser is not running", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	request := r.Clone(r.Context())
	request.URL.Path = cdpUpstreamPath(r.URL.Path)
	request.URL.RawPath = ""
	request.URL.RawQuery = cdpQueryWithoutToken(r.URL.Query())
	request.Host = net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
	request.RequestURI = ""
	request.Header.Del("Authorization")
	request.Header.Del("Cookie")
	request.Header.Del("Origin")
	if err := request.Write(upstream); err != nil {
		return
	}
	reader := bufio.NewReader(upstream)
	response, err := http.ReadResponse(reader, request)
	if err != nil {
		http.Error(w, "Browser WebSocket handshake failed", http.StatusBadGateway)
		return
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		defer response.Body.Close()
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSockets need HTTP/1.1", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\n")
	_ = response.Header.Write(buffer)
	_, _ = io.WriteString(buffer, "\r\n")
	if err := buffer.Flush(); err != nil {
		client.Close()
		return
	}
	a.browser.cdpConnection(true)
	defer a.browser.cdpConnection(false)
	a.browserPipe(client, buffer, upstream, reader)
}

func (m *browserManager) cdpConnection(active bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if active {
		m.connections++
	} else if m.connections > 0 {
		m.connections--
	}
}

func (a *app) browserScreen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet || !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		http.Error(w, "Browser screen needs a WebSocket", http.StatusBadRequest)
		return
	}
	state := a.browser.status()
	if !state.Running {
		http.Error(w, "Start the browser first", http.StatusConflict)
		return
	}
	page, ok := browserPageFor(state.Pages, r.URL.Query().Get("page"))
	if !ok {
		http.Error(w, "No browser page is open", http.StatusNotFound)
		return
	}
	upstream, reader, _, err := dialCDPWebsocket(r.Context(), page.WebSocketDebuggerURL)
	if err != nil {
		http.Error(w, "Browser screen connection failed", http.StatusBadGateway)
		return
	}
	defer upstream.Close()
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		http.Error(w, "WebSockets need HTTP/1.1", http.StatusInternalServerError)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		return
	}
	defer client.Close()
	fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\n")
	fmt.Fprintf(buffer, "Upgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Accept: %s\r\n", websocketAccept(r.Header.Get("Sec-WebSocket-Key")))
	_, _ = io.WriteString(buffer, "\r\n")
	if err := buffer.Flush(); err != nil {
		return
	}
	start, _ := json.Marshal(object{"id": 1, "method": "Page.startScreencast", "params": object{"format": "jpeg", "quality": 60, "maxWidth": 1280, "everyNthFrame": 1}})
	var upstreamWrite sync.Mutex
	if err := func() error {
		upstreamWrite.Lock()
		defer upstreamWrite.Unlock()
		return writeWebSocketFrame(upstream, start)
	}(); err != nil {
		return
	}
	a.screenPipe(client, buffer, upstream, reader, &upstreamWrite)
}

func dialCDPWebsocket(ctx context.Context, wsURL string) (net.Conn, *bufio.Reader, *http.Response, error) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Scheme != "ws" || u.Host == "" || u.Path == "" {
		return nil, nil, nil, fmt.Errorf("invalid CDP WebSocket URL")
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, nil, nil, err
	}
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	query := ""
	if u.RawQuery != "" {
		query = "?" + u.RawQuery
	}
	if _, err := fmt.Fprintf(conn, "GET %s%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.EscapedPath(), query, u.Host, key); err != nil {
		conn.Close()
		return nil, nil, nil, err
	}
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet})
	if err != nil || response.StatusCode != http.StatusSwitchingProtocols {
		conn.Close()
		if err == nil {
			err = fmt.Errorf("CDP returned HTTP %d", response.StatusCode)
		}
		return nil, nil, nil, err
	}
	return conn, reader, response, nil
}

func (a *app) screenPipe(client net.Conn, clientReader *bufio.ReadWriter, upstream net.Conn, upstreamReader *bufio.Reader, upstreamWrite *sync.Mutex) {
	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		lastFrame := time.Time{}
		for {
			opcode, payload, err := readWebSocketFrame(upstreamReader)
			if err != nil {
				log.Printf("screen: upstream read: %v", err)
				return
			}
			if opcode == 0x9 {
				upstreamWrite.Lock()
				_ = writeWebSocketControl(upstream, 0xA, payload)
				upstreamWrite.Unlock()
				continue
			}
			if opcode != 0x1 {
				continue
			}
			var message struct {
				Method string `json:"method"`
				Params struct {
					SessionID string `json:"sessionId"`
				} `json:"params"`
			}
			_ = json.Unmarshal(payload, &message)
			if message.Method == "Page.screencastFrame" && message.Params.SessionID != "" {
				ack, _ := json.Marshal(object{"id": 2, "method": "Page.screencastFrameAck", "params": object{"sessionId": message.Params.SessionID}})
				upstreamWrite.Lock()
				_ = writeWebSocketFrame(upstream, ack)
				upstreamWrite.Unlock()
				if !lastFrame.IsZero() && time.Since(lastFrame) < 100*time.Millisecond {
					continue
				}
				lastFrame = time.Now()
			}
			_ = writeWebSocketFrameWithOpcode(client, 0x1, payload, false)
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for {
			opcode, payload, err := readWebSocketFrame(clientReader.Reader)
			if err != nil {
				log.Printf("screen: client read: %v", err)
				return
			}
			if opcode == 0x9 {
				_ = writeWebSocketControl(client, 0xA, payload)
				continue
			}
			if opcode != 0x1 && opcode != 0x2 {
				continue
			}
			upstreamWrite.Lock()
			err = writeWebSocketFrameWithOpcode(upstream, opcode, payload, true)
			upstreamWrite.Unlock()
			if err != nil {
				return
			}
		}
	}()
	select {
	case <-a.ctx.Done():
	case <-done:
	}
	client.Close()
	upstream.Close()
}

func (a *app) browserPipe(client net.Conn, clientReader io.Reader, upstream net.Conn, upstreamReader io.Reader) {
	pipe(a.ctx, client, clientReader, upstream, upstreamReader)
}

func captureCDPScreenshot(ctx context.Context, wsURL string) ([]byte, error) {
	u, err := url.Parse(wsURL)
	if err != nil || u.Scheme != "ws" || u.Host == "" || u.Path == "" {
		return nil, fmt.Errorf("invalid CDP page URL")
	}
	dialer := net.Dialer{Timeout: 3 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", u.Host)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		return nil, err
	}
	key := base64.StdEncoding.EncodeToString(keyBytes)
	query := ""
	if u.RawQuery != "" {
		query = "?" + u.RawQuery
	}
	fmt.Fprintf(conn, "GET %s%s HTTP/1.1\r\nHost: %s\r\nUpgrade: websocket\r\nConnection: Upgrade\r\nSec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n", u.EscapedPath(), query, u.Host, key)
	reader := bufio.NewReader(conn)
	response, err := http.ReadResponse(reader, &http.Request{Method: http.MethodGet, Header: http.Header{"Upgrade": {"websocket"}}})
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusSwitchingProtocols {
		response.Body.Close()
		return nil, fmt.Errorf("CDP returned HTTP %d", response.StatusCode)
	}
	request := object{"id": 1, "method": "Page.captureScreenshot", "params": object{"format": "png"}}
	message, _ := json.Marshal(request)
	if err := writeWebSocketFrame(conn, message); err != nil {
		return nil, err
	}
	for {
		opcode, payload, err := readWebSocketFrame(reader)
		if err != nil {
			return nil, err
		}
		if opcode == 0x9 {
			if err := writeWebSocketControl(conn, 0xA, payload); err != nil {
				return nil, err
			}
			continue
		}
		if opcode != 0x1 {
			continue
		}
		var result struct {
			ID     int `json:"id"`
			Result struct {
				Data string `json:"data"`
			} `json:"result"`
			Error any `json:"error"`
		}
		if err := json.Unmarshal(payload, &result); err != nil || result.ID != 1 {
			continue
		}
		if result.Error != nil {
			return nil, fmt.Errorf("CDP screenshot failed")
		}
		return base64.StdEncoding.DecodeString(result.Result.Data)
	}
}

func writeWebSocketFrame(w io.Writer, payload []byte) error {
	return writeWebSocketFrameWithOpcode(w, 0x1, payload, true)
}

func writeWebSocketControl(w io.Writer, opcode byte, payload []byte) error {
	return writeWebSocketFrameWithOpcode(w, opcode, payload, true)
}

func writeWebSocketFrameWithOpcode(w io.Writer, opcode byte, payload []byte, mask bool) error {
	if len(payload) > 125 && opcode >= 8 {
		return fmt.Errorf("control frame too large")
	}
	first := byte(0x80 | opcode)
	if _, err := w.Write([]byte{first}); err != nil {
		return err
	}
	length := len(payload)
	second := byte(0)
	if mask {
		second |= 0x80
	}
	switch {
	case length < 126:
		if _, err := w.Write([]byte{second | byte(length)}); err != nil {
			return err
		}
	case length <= 65535:
		if _, err := w.Write([]byte{second | 126}); err != nil {
			return err
		}
		var size [2]byte
		binary.BigEndian.PutUint16(size[:], uint16(length))
		if _, err := w.Write(size[:]); err != nil {
			return err
		}
	default:
		if _, err := w.Write([]byte{second | 127}); err != nil {
			return err
		}
		var size [8]byte
		binary.BigEndian.PutUint64(size[:], uint64(length))
		if _, err := w.Write(size[:]); err != nil {
			return err
		}
	}
	if !mask {
		_, err := w.Write(payload)
		return err
	}
	maskKey := [4]byte{}
	if _, err := rand.Read(maskKey[:]); err != nil {
		return err
	}
	if _, err := w.Write(maskKey[:]); err != nil {
		return err
	}
	masked := make([]byte, len(payload))
	for i, value := range payload {
		masked[i] = value ^ maskKey[i%4]
	}
	_, err := w.Write(masked)
	return err
}

func readWebSocketFrame(reader *bufio.Reader) (byte, []byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	second, err := reader.ReadByte()
	if err != nil {
		return 0, nil, err
	}
	length := uint64(second & 0x7f)
	if length == 126 {
		var size [2]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return 0, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(size[:]))
	} else if length == 127 {
		var size [8]byte
		if _, err := io.ReadFull(reader, size[:]); err != nil {
			return 0, nil, err
		}
		length = binary.BigEndian.Uint64(size[:])
	}
	if length > 8<<20 {
		return 0, nil, fmt.Errorf("WebSocket frame too large")
	}
	var mask [4]byte
	if second&0x80 != 0 {
		if _, err := io.ReadFull(reader, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, int(length))
	if _, err := io.ReadFull(reader, payload); err != nil {
		return 0, nil, err
	}
	if second&0x80 != 0 {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return first & 0x0f, payload, nil
}

func websocketAccept(key string) string {
	hash := sha1.Sum([]byte(key + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	return base64.StdEncoding.EncodeToString(hash[:])
}
