package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

//go:embed index.html
var page []byte
var version = "dev"

type app struct {
	cfg           config
	secret        []byte
	ctx           context.Context
	cancel        context.CancelFunc
	collect       *collectors
	health        *healthSampler
	mirrors       *mirrorManager
	term          terminalManager
	boxesMemo     boxCache
	usage         *usageService
	workers       sync.WaitGroup
	live          *liveHub
	browser       *browserManager
	peers         *peerDiscovery
	gitStatusMu   sync.Mutex
	gitStatusMemo map[string]gitStatusCache
}

func jsonEncode(w io.Writer, v any) error { return json.NewEncoder(w).Encode(v) }
func pageStyle() string {
	s := string(page)
	start := strings.Index(s, "<style>") + len("<style>")
	end := strings.Index(s, "</style>")
	return s[start:end]
}
func fileURL(relative string) string {
	return (&url.URL{Path: "/files/" + filepath.ToSlash(relative)}).String()
}
func newApp(cfg config, secret []byte) *app {
	ctx, cancel := context.WithCancel(context.Background())
	a := &app{cfg: cfg, secret: secret, ctx: ctx, cancel: cancel, gitStatusMemo: map[string]gitStatusCache{}}
	a.live = newLiveHub(ctx, cfg.home)
	a.browser = newBrowserManager(cfg.home, ctx)
	a.peers = &peerDiscovery{}
	a.collect = &collectors{ctx: ctx, cfg: cfg, titles: map[int]titleEntry{}}
	a.health = &healthSampler{ctx: ctx, hist: map[string][]float64{"cpu": {}, "mem": {}, "swap": {}, "load": {}}}
	a.mirrors = &mirrorManager{ctx: ctx, cfg: cfg, entries: map[int]mirrorEntry{}}
	a.usage = newUsageService(cfg)
	a.boxesMemo.failures = map[string]time.Time{}
	return a
}
func (a *app) state() object {
	var ports []portInfo
	var procs []process
	var panes []tmuxPane
	var docker, reports []object
	var herdr herdrState
	var wg sync.WaitGroup
	for _, fn := range []func(){func() { ports = a.collect.getPorts() }, func() { procs = a.collect.getProcesses() }, func() { panes = a.collect.getPanes() }, func() { docker = a.collect.getDocker() }, func() { reports = a.collect.getReports() }, func() { herdr = a.collect.getHerdr() }} {
		wg.Add(1)
		go func() { defer wg.Done(); fn() }()
	}
	wg.Wait()
	h, hist := a.health.snapshot()
	mirrors, mirrorErrors := a.mirrors.snapshot()
	return object{"title": a.cfg.Title, "health": h, "hist": hist, "ports": ports, "agents": mergeAgents(procs, panes, herdr, a.cfg, paneFromEnvironment), "tmux": tmuxSessions(panes), "browsers": browserProcesses(procs), "docker": docker, "reports": reports, "quick": a.cfg.Quick, "host": a.cfg.Host, "mirror": mirrors, "now": time.Now().UnixMilli(), "herdr": herdr, "terminal": a.terminalStatus(), "mirrorErrors": mirrorErrors, "reportDays": a.cfg.ReportDays}
}
func safeURLPath(path string) bool {
	depth := 0
	for _, part := range strings.Split(path, "/") {
		switch part {
		case "", ".":
		case "..":
			depth--
			if depth < 0 {
				return false
			}
		default:
			depth++
		}
	}
	return !strings.ContainsRune(path, 0)
}
func (a *app) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if !safeURLPath(r.URL.Path) {
		http.Error(w, "forbidden", 403)
		return
	}
	if r.URL.Path == "/login" {
		a.login(w, r)
		return
	}
	if r.URL.Path == "/api/health" {
		a.healthAPI(w, r)
		return
	}
	if !a.authed(r) {
		a.unauthorized(w, r)
		return
	}
	if r.Method != "GET" && r.Method != "HEAD" && !sameOrigin(r) {
		jsonReply(w, 403, object{"error": "Open the deck and try again"})
		return
	}
	switch {
	case r.URL.Path == "/logout":
		if r.Method != "POST" {
			w.Header().Set("Allow", "POST")
			http.Error(w, "Use Sign out on the deck", 405)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "boxdeck", Value: "", Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1, Expires: time.Unix(1, 0)})
		http.Redirect(w, r, "/login", 303)
	case r.URL.Path == "/api/stream":
		a.stream(w, r)
	case r.URL.Path == "/api/proc/kill":
		a.killProc(w, r)
	case r.URL.Path == "/api/ui/procs":
		a.uiProcs(w, r)
	case r.URL.Path == "/api/ui/settings":
		a.uiSettings(w, r)
	case r.URL.Path == "/api/usage":
		a.usageAPI(w, r)
	case r.URL.Path == "/api/usage/all":
		a.usageAllAPI(w, r)
	case r.URL.Path == "/api/edit/new":
		a.editNew(w, r)
	case r.URL.Path == "/api/edit/rename":
		a.editRename(w, r)
	case r.URL.Path == "/api/edit":
		a.edit(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/git/"):
		a.gitAPI(w, r)
	case r.URL.Path == "/api/state":
		if r.Method != "GET" {
			jsonReply(w, 405, object{"error": "Use GET to read state"})
			return
		}
		a.health.lastPoll.Store(time.Now().UnixMilli())
		jsonReply(w, 200, a.state())
	case r.URL.Path == "/api/herdr/focus":
		a.focusHerdr(w, r)
	case r.URL.Path == "/api/herd/read":
		a.herdRead(w, r)
	case r.URL.Path == "/api/herd/interrupt":
		a.herdInterrupt(w, r)
	case r.URL.Path == "/api/herd/keys":
		a.herdKeys(w, r)
	case r.URL.Path == "/api/herd/prompt":
		a.herdPrompt(w, r)
	case r.URL.Path == "/api/browser":
		a.browserStatus(w, r)
	case r.URL.Path == "/api/browser/start":
		a.browserStart(w, r)
	case r.URL.Path == "/api/browser/stop":
		a.browserStop(w, r)
	case r.URL.Path == "/api/browser/shot":
		a.browserShot(w, r)
	case r.URL.Path == "/api/browser/tabs":
		a.browserTabs(w, r)
	case r.URL.Path == "/api/browser/screen":
		a.browserScreen(w, r)
	case r.URL.Path == "/api/net":
		a.netAPI(w, r)
	case r.URL.Path == "/api/net/peers":
		a.netPeersAPI(w, r)
	case r.URL.Path == "/views-net.js" || r.URL.Path == "/views-net.css":
		a.netViewAsset(w, r)
	case r.URL.Path == "/cdp" || strings.HasPrefix(r.URL.Path, "/cdp/"):
		a.cdp(w, r)
	case strings.HasPrefix(r.URL.Path, "/api/"):
		a.api(w, r)
	case r.URL.Path == "/files" || r.URL.Path == "/term":
		http.Redirect(w, r, r.URL.Path+"/", 302)
	case strings.HasPrefix(r.URL.Path, "/files/"):
		a.files(w, r)
	case strings.HasPrefix(r.URL.Path, "/term/"):
		a.terminal(w, r)
	case r.URL.Path == "/" || r.URL.Path == "/index.html":
		if r.Method != "GET" && r.Method != "HEAD" {
			http.Error(w, "Use GET to open the deck", 405)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "HEAD" {
			w.Write(page)
		}
	case strings.HasPrefix(r.URL.Path, "/api/"):
		jsonReply(w, 404, object{"error": "API route not found"})
	default:
		http.Error(w, "Page not found. Open / to return to the deck.", 404)
	}
}
func (a *app) focusHerdr(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonReply(w, 405, object{"error": "Use POST to focus a pane"})
		return
	}
	var body struct {
		PaneID string `json:"pane_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.PaneID == "" {
		jsonReply(w, 400, object{"error": "Choose an agent pane and try again"})
		return
	}
	h := a.collect.getHerdr()
	if !h.Running {
		jsonReply(w, 404, object{"error": "Herdr is not running. Start herdr on the box"})
		return
	}
	found := false
	for _, agent := range h.Agents {
		if agent.PaneID == body.PaneID {
			found = true
		}
	}
	if !found {
		jsonReply(w, 404, object{"error": "This pane has closed. Refresh and choose an agent"})
		return
	}
	b, err := herdrRequest(r.Context(), herdrSocket(a.cfg.home), "agent.focus", object{"target": body.PaneID})
	var result struct {
		Error  json.RawMessage `json:"error"`
		Result json.RawMessage `json:"result"`
	}
	if err != nil || json.Unmarshal(b, &result) != nil || len(result.Error) > 0 || len(result.Result) == 0 {
		jsonReply(w, 502, object{"error": "Could not focus this pane. Refresh and try again"})
		return
	}
	jsonReply(w, 200, object{"focused": body.PaneID})
}
func serve(cfg config) error {
	secret, err := loadSecret(filepath.Join(filepath.Dir(cfg.path), "secret"))
	if err != nil {
		return err
	}
	a := newApp(cfg, secret)
	a.usage.start(a.ctx)
	defer a.cancel()
	defer a.mirrors.close()
	defer a.browser.stop()
	listener, err := net.Listen("tcp", cfg.address(cfg.Port))
	if err != nil {
		return err
	}
	servers := []*http.Server{}
	listeners := []net.Listener{listener}
	if cfg.FilesPort > 0 {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", fmt.Sprint(cfg.FilesPort)))
		if err != nil {
			listener.Close()
			return fmt.Errorf("files compatibility port: %w", err)
		}
		listeners = append(listeners, l)
	}
	a.health.sample()
	a.startTerminal()
	a.workers.Add(2)
	go func() { defer a.workers.Done(); a.health.run() }()
	go func() {
		defer a.workers.Done()
		if cfg.Mirror == false || mirrorIP(cfg) == "" {
			return
		}
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		a.mirrors.sync(a.collect.getPorts())
		for {
			select {
			case <-a.ctx.Done():
				return
			case <-ticker.C:
				// Like the Node mirror, discover listeners even with no deck open.
				a.mirrors.sync(a.collect.getPorts())
			}
		}
	}()
	errors := make(chan error, len(listeners))
	for i, l := range listeners {
		var handler http.Handler = a
		if i > 0 {
			handler = http.HandlerFunc(a.oldFiles)
		}
		srv := &http.Server{Handler: handler, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
		servers = append(servers, srv)
		go func() { errors <- srv.Serve(l) }()
	}
	fmt.Printf("boxdeck %s on http://%s\n", version, cfg.address(cfg.Port))
	stop, stopSignals := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stopSignals()
	select {
	case <-stop.Done():
	case err = <-errors:
		if err == http.ErrServerClosed {
			err = nil
		}
	}
	a.cancel()
	a.mirrors.close()
	shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, srv := range servers {
		if e := srv.Shutdown(shutdown); e != nil {
			srv.Close()
		}
	}
	a.workers.Wait()
	return err
}
func main() {
	command := "serve"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	if command == "version" {
		fmt.Println("boxdeck " + version)
		return
	}
	var err error
	if command == "ctl" {
		err = ctlCommand(os.Args[2:])
	} else if command == "usage" {
		err = usageCommand(os.Args[2:])
	} else if command == "token" {
		err = tokenCommand(os.Args[2:])
	} else if command == "install" {
		err = install(os.Args[2:]...)
	} else if command == "serve" {
		home, e := os.UserHomeDir()
		err = e
		if err == nil {
			var cfg config
			cfg, err = loadConfig(configPath(home), home)
			if err == nil {
				err = serve(cfg)
			}
		}
	} else {
		err = fmt.Errorf("usage: boxdeck [serve|install|version|token|ctl|usage]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "boxdeck:", err)
		os.Exit(1)
	}
}
