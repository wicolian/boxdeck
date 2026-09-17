package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

type terminalState struct {
	Available bool   `json:"available"`
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}
type terminalManager struct {
	mu        sync.Mutex
	err       string
	installed bool
	status    memo[bool]
}

func (a *app) startTerminal() {
	path, err := exec.LookPath("ttyd")
	a.term.installed = err == nil
	if !a.cfg.Terminal {
		return
	}
	if err != nil {
		a.term.err = "Install ttyd to open a terminal"
		return
	}
	cmd := exec.CommandContext(a.ctx, path, "-p", strconv.Itoa(int(a.cfg.TTYDPort)), "-i", "127.0.0.1", "-W", "--base-path", "/term", "tmux", "new-session", "-A", "-s", "web")
	cmd.WaitDelay = time.Second
	if err = cmd.Start(); err != nil {
		a.term.err = "Could not start ttyd: " + err.Error()
		return
	}
	a.workers.Add(1)
	go func() {
		defer a.workers.Done()
		err := cmd.Wait()
		if a.ctx.Err() == nil {
			a.term.mu.Lock()
			a.term.err = fmt.Sprintf("ttyd stopped (%v). Check port %d and start boxdeck again", err, a.cfg.TTYDPort)
			a.term.mu.Unlock()
		}
	}()
}
func (a *app) terminalStatus() terminalState {
	available := a.term.status.get(3*time.Second, func() bool {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(a.cfg.TTYDPort))), 150*time.Millisecond)
		if err != nil {
			return false
		}
		conn.Close()
		return true
	})
	a.term.mu.Lock()
	defer a.term.mu.Unlock()
	return terminalState{Available: available, Installed: a.term.installed, Error: a.term.err}
}
func (a *app) terminal(w http.ResponseWriter, r *http.Request) {
	if !sameOrigin(r) {
		http.Error(w, "Open Terminal from the deck", 403)
		return
	}
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		a.terminalWebsocket(w, r)
		return
	}
	target := &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(a.cfg.TTYDPort)))}
	proxy := httputil.ReverseProxy{Rewrite: func(p *httputil.ProxyRequest) {
		p.SetURL(target)
		p.Out.Header.Del("Authorization")
		p.Out.Header.Del("Cookie")
		p.Out.Host = target.Host
	}, ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(502)
		io.WriteString(w, fileDocument("Terminal unavailable", "<h2>Terminal is not running</h2><p>Install ttyd and tmux, then enable terminal in the boxdeck config.</p>"))
	}}
	proxy.ServeHTTP(w, r)
}
func (a *app) terminalWebsocket(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "Use GET for a WebSocket", 405)
		return
	}
	upstream, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(a.cfg.TTYDPort))), 2*time.Second)
	if err != nil {
		http.Error(w, "Terminal is not running; start ttyd and try again", 502)
		return
	}
	_ = upstream.SetDeadline(time.Now().Add(5 * time.Second))
	out := r.Clone(r.Context())
	out.RequestURI = ""
	out.Header.Del("Authorization")
	out.Header.Del("Cookie")
	// Keep the public Host and Origin together for ttyd's optional origin check.
	if err = out.Write(upstream); err != nil {
		upstream.Close()
		http.Error(w, "Terminal connection failed", 502)
		return
	}
	reader := bufio.NewReader(upstream)
	res, err := http.ReadResponse(reader, out)
	if err != nil {
		upstream.Close()
		http.Error(w, "Terminal handshake failed", 502)
		return
	}
	if res.StatusCode != http.StatusSwitchingProtocols {
		defer upstream.Close()
		defer res.Body.Close()
		for k, v := range res.Header {
			w.Header()[k] = v
		}
		w.WriteHeader(res.StatusCode)
		io.Copy(w, res.Body)
		return
	}
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		upstream.Close()
		http.Error(w, "WebSockets need HTTP/1.1", 500)
		return
	}
	client, buffer, err := hijacker.Hijack()
	if err != nil {
		upstream.Close()
		return
	}
	fmt.Fprintf(buffer, "HTTP/1.1 101 Switching Protocols\r\n")
	res.Header.Write(buffer)
	io.WriteString(buffer, "\r\n")
	if err = buffer.Flush(); err != nil {
		client.Close()
		upstream.Close()
		return
	}
	_ = upstream.SetDeadline(time.Time{})
	// Both readers may already contain WebSocket bytes after their HTTP headers.
	pipe(a.ctx, client, buffer, upstream, reader)
}
