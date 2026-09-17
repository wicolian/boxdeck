package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The built-in recipes are deliberately data files. Adding a recipe should not
// require a Go change or a change to the Apps view.
//
//go:embed apps/*.json
var embeddedAppRecipes embed.FS

var appIDRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

type appRecipe struct {
	ID      string     `json:"id"`
	Name    string     `json:"name"`
	Tag     string     `json:"tag"`
	Detect  appDetect  `json:"detect"`
	Install appInstall `json:"install"`
	Start   appStart   `json:"start"`
	Stop    appStop    `json:"stop"`
	Open    appOpen    `json:"open"`
	Health  appHealth  `json:"health"`
	Docs    string     `json:"docs"`
}

type appDetect struct {
	Bin  string     `json:"bin"`
	Port portNumber `json:"port"`
	File string     `json:"file"`
}

type appInstall struct {
	Hint string `json:"hint"`
	URL  string `json:"url"`
}

type appStart struct {
	Cmd  []string   `json:"cmd"`
	Port portNumber `json:"port"`
}

type appStop struct {
	Mode string
	Cmd  []string
}

func (s *appStop) UnmarshalJSON(b []byte) error {
	var mode string
	if err := json.Unmarshal(b, &mode); err == nil {
		s.Mode = mode
		s.Cmd = nil
		return nil
	}
	var cmd []string
	if err := json.Unmarshal(b, &cmd); err == nil {
		s.Mode = "command"
		s.Cmd = cmd
		return nil
	}
	var value struct {
		Mode string   `json:"mode"`
		Cmd  []string `json:"cmd"`
	}
	if err := json.Unmarshal(b, &value); err != nil {
		return fmt.Errorf("stop must be signal, a command array, or an object: %w", err)
	}
	s.Mode, s.Cmd = value.Mode, value.Cmd
	if s.Mode == "" && len(s.Cmd) > 0 {
		s.Mode = "command"
	}
	return nil
}

func (s appStop) MarshalJSON() ([]byte, error) {
	if len(s.Cmd) > 0 {
		return json.Marshal(s.Cmd)
	}
	if s.Mode == "" {
		return json.Marshal("signal")
	}
	return json.Marshal(s.Mode)
}

type appOpen struct {
	URL   string `json:"url"`
	Path  string `json:"path"`
	Embed bool   `json:"embed"`
}

type appHealth struct {
	HTTP string     `json:"http"`
	Port portNumber `json:"port"`
}

type appTemplateValues struct {
	Home string
	ID   string
	Port int
	Host string
}

func expandAppTemplate(value string, values appTemplateValues) string {
	return strings.NewReplacer(
		"{home}", values.Home,
		"{id}", values.ID,
		"{port}", strconv.Itoa(values.Port),
		"{host}", values.Host,
	).Replace(value)
}

func appRecipeDir(cfg config) string {
	return filepath.Join(cfg.home, ".config", "boxdeck", "apps")
}

func loadAppRecipes(cfg config) (map[string]appRecipe, error) {
	result := map[string]appRecipe{}
	var firstErr error
	load := func(name string, data []byte) {
		var recipe appRecipe
		if err := json.Unmarshal(data, &recipe); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("read app recipe %s: %w", name, err)
			}
			return
		}
		if err := validateAppRecipe(recipe); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("app recipe %s: %w", name, err)
			}
			return
		}
		result[recipe.ID] = recipe
	}

	names, err := fs.Glob(embeddedAppRecipes, "apps/*.json")
	if err != nil {
		return result, err
	}
	sort.Strings(names)
	for _, name := range names {
		data, readErr := fs.ReadFile(embeddedAppRecipes, name)
		if readErr != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("read embedded app recipe %s: %w", name, readErr)
			}
			continue
		}
		load(name, data)
	}

	// Config recipes override the embedded defaults. Home recipes are loaded
	// last so a dropped-in file is the final, deterministic override.
	for _, recipe := range cfg.Apps {
		if err := validateAppRecipe(recipe); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("config app recipe: %w", err)
			}
			continue
		}
		result[recipe.ID] = recipe
	}
	entries, readErr := os.ReadDir(appRecipeDir(cfg))
	if readErr == nil {
		for _, entry := range entries {
			if entry.IsDir() || strings.ToLower(filepath.Ext(entry.Name())) != ".json" {
				continue
			}
			path := filepath.Join(appRecipeDir(cfg), entry.Name())
			data, err := os.ReadFile(path)
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("read app recipe %s: %w", path, err)
				}
				continue
			}
			load(path, data)
		}
	} else if !os.IsNotExist(readErr) && firstErr == nil {
		firstErr = fmt.Errorf("read app recipe directory: %w", readErr)
	}
	return result, firstErr
}

func validateAppRecipe(recipe appRecipe) error {
	recipe.ID = strings.TrimSpace(recipe.ID)
	if !appIDRE.MatchString(recipe.ID) {
		return fmt.Errorf("id must contain lowercase letters, numbers, underscores, or hyphens")
	}
	if strings.TrimSpace(recipe.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if len(recipe.Start.Cmd) == 0 && recipe.Start.Port != 0 {
		return fmt.Errorf("start.port needs start.cmd")
	}
	if recipe.Start.Port < 0 || recipe.Start.Port > 65535 || recipe.Detect.Port > 65535 || recipe.Health.Port > 65535 {
		return fmt.Errorf("port must be from 0 to 65535")
	}
	if recipe.Stop.Mode != "" && recipe.Stop.Mode != "signal" && recipe.Stop.Mode != "command" && len(recipe.Stop.Cmd) == 0 {
		return fmt.Errorf("stop must be signal or a command")
	}
	if len(recipe.Stop.Cmd) > 0 {
		recipe.Stop.Mode = "command"
	}
	return nil
}

func detectApp(recipe appRecipe, home string) bool {
	if recipe.Detect.Bin != "" {
		if _, err := lookPathWide(recipe.Detect.Bin); err == nil {
			return true
		}
	}
	if recipe.Detect.Port > 0 {
		conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(recipe.Detect.Port))), 150*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
	}
	if recipe.Detect.File != "" {
		_, err := os.Stat(expandAppTemplate(recipe.Detect.File, appTemplateValues{Home: home}))
		return err == nil
	}
	return false
}

type appRuntime struct {
	Port int
}

func checkAppHealth(recipe appRecipe, runtime appRuntime) bool {
	values := appTemplateValues{Port: runtime.Port}
	if recipe.Health.HTTP != "" {
		client := &http.Client{Timeout: 2 * time.Second}
		response, err := client.Get(expandAppTemplate(recipe.Health.HTTP, values))
		if err != nil {
			return false
		}
		_ = response.Body.Close()
		return response.StatusCode >= 200 && response.StatusCode < 400
	}
	port := runtime.Port
	if port == 0 {
		port = int(recipe.Health.Port)
	}
	if port == 0 {
		port = int(recipe.Detect.Port)
	}
	if port == 0 {
		return false
	}
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type appView struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Tag         string   `json:"tag,omitempty"`
	Status      string   `json:"status"`
	Detected    bool     `json:"detected"`
	Running     bool     `json:"running"`
	PID         int      `json:"pid,omitempty"`
	Port        int      `json:"port,omitempty"`
	URL         string   `json:"url,omitempty"`
	Health      bool     `json:"health"`
	Log         []string `json:"log,omitempty"`
	InstallHint string   `json:"installHint,omitempty"`
	InstallURL  string   `json:"installURL,omitempty"`
	Docs        string   `json:"docs,omitempty"`
	OpenPath    string   `json:"openPath,omitempty"`
	Embed       bool     `json:"embed"`
	CanStart    bool     `json:"canStart"`
	CanStop     bool     `json:"canStop"`
	Message     string   `json:"message,omitempty"`
	ExitCode    *int     `json:"exitCode,omitempty"`
}

type appExtras struct {
	TerminalRunning bool
	BrowserRunning  bool
}

type managedApp struct {
	cmd       *exec.Cmd
	pid       int
	port      int
	logPath   string
	startedAt time.Time
	exitCode  *int
	exitError string
	done      chan struct{}
	stopping  bool
}

type appManager struct {
	mu        sync.Mutex
	ctx       context.Context
	cfg       config
	recipes   map[string]appRecipe
	processes map[string]*managedApp
	loadErr   error
}

func newAppsManager(ctx context.Context, cfg config) *appManager {
	recipes, err := loadAppRecipes(cfg)
	return &appManager{ctx: ctx, cfg: cfg, recipes: recipes, processes: map[string]*managedApp{}, loadErr: err}
}

func (m *appManager) reload() error {
	recipes, err := loadAppRecipes(m.cfg)
	m.mu.Lock()
	m.recipes = recipes
	m.loadErr = err
	m.mu.Unlock()
	return err
}

func (m *appManager) start(id string) (appView, error) {
	m.mu.Lock()
	recipe, ok := m.recipes[id]
	if !ok {
		m.mu.Unlock()
		return appView{}, fmt.Errorf("unknown app %q", id)
	}
	if current := m.processes[id]; current != nil && current.exitCode == nil {
		view := m.viewLocked(recipe, current, appExtras{})
		m.mu.Unlock()
		return view, fmt.Errorf("app %q is already running", id)
	}
	if len(recipe.Start.Cmd) == 0 {
		m.mu.Unlock()
		if id == "browser" {
			return appView{}, errors.New("Browser start comes with the Browser view")
		}
		return appView{}, fmt.Errorf("app %q has no managed start command", id)
	}
	port := int(recipe.Start.Port)
	if port == 0 {
		var err error
		port, err = freeAppPort()
		if err != nil {
			m.mu.Unlock()
			return appView{}, err
		}
	}
	values := appTemplateValues{Home: m.cfg.home, ID: recipe.ID, Port: port, Host: m.cfg.Host}
	args := make([]string, len(recipe.Start.Cmd))
	for i, arg := range recipe.Start.Cmd {
		args[i] = expandAppTemplate(arg, values)
	}
	logPath := filepath.Join(m.cfg.home, ".local", "share", "boxdeck", "apps", recipe.ID+".log")
	if err := os.MkdirAll(filepath.Dir(logPath), 0700); err != nil {
		m.mu.Unlock()
		return appView{}, fmt.Errorf("create app log directory: %w", err)
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		m.mu.Unlock()
		return appView{}, fmt.Errorf("open app log: %w", err)
	}
	if p, err := lookPathWide(args[0]); err == nil {
		args[0] = p
	}
	cmd := exec.CommandContext(m.ctx, args[0], args[1:]...)
	cmd.Dir = m.cfg.home
	cmd.Stdout, cmd.Stderr = logFile, logFile
	configureAppProcess(cmd)
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		m.mu.Unlock()
		return appView{}, fmt.Errorf("start app %q: %w", id, err)
	}
	managed := &managedApp{cmd: cmd, pid: cmd.Process.Pid, port: port, logPath: logPath, startedAt: time.Now(), done: make(chan struct{})}
	if m.processes == nil {
		m.processes = map[string]*managedApp{}
	}
	m.processes[id] = managed
	m.mu.Unlock()
	go func() {
		err := cmd.Wait()
		_ = logFile.Close()
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		m.mu.Lock()
		if current := m.processes[id]; current == managed {
			managed.exitCode = &exitCode
			if err != nil {
				managed.exitError = err.Error()
			}
			close(managed.done)
		}
		m.mu.Unlock()
	}()
	return m.view(id, appExtras{}), nil
}

func freeAppPort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, fmt.Errorf("choose app port: %w", err)
	}
	defer listener.Close()
	return listener.Addr().(*net.TCPAddr).Port, nil
}

func (m *appManager) stop(id string) error {
	m.mu.Lock()
	recipe, ok := m.recipes[id]
	managed := m.processes[id]
	if !ok {
		m.mu.Unlock()
		return fmt.Errorf("unknown app %q", id)
	}
	if managed == nil || managed.exitCode != nil {
		m.mu.Unlock()
		return fmt.Errorf("app %q is not running", id)
	}
	managed.stopping = true
	m.mu.Unlock()

	if len(recipe.Stop.Cmd) > 0 || recipe.Stop.Mode == "command" {
		values := appTemplateValues{Home: m.cfg.home, ID: recipe.ID, Port: managed.port, Host: m.cfg.Host}
		args := make([]string, len(recipe.Stop.Cmd))
		for i, arg := range recipe.Stop.Cmd {
			args[i] = expandAppTemplate(arg, values)
		}
		if len(args) == 0 {
			return fmt.Errorf("app %q has an empty stop command", id)
		}
		ctx, cancel := context.WithTimeout(m.ctx, 10*time.Second)
		cmd := exec.CommandContext(ctx, args[0], args[1:]...)
		cmd.Dir = m.cfg.home
		if err := cmd.Run(); err != nil {
			cancel()
			return fmt.Errorf("stop app %q: %w", id, err)
		}
		cancel()
	} else {
		if err := terminateAppProcess(managed.cmd); err != nil {
			return fmt.Errorf("stop app %q: %w", id, err)
		}
	}
	select {
	case <-managed.done:
		return nil
	case <-time.After(10 * time.Second):
		if err := killAppProcess(managed.cmd); err != nil {
			return fmt.Errorf("kill app %q: %w", id, err)
		}
		<-managed.done
		return nil
	}
}

func (m *appManager) restart(id string) (appView, error) {
	if err := m.stop(id); err != nil && !strings.Contains(err.Error(), "not running") {
		return appView{}, err
	}
	return m.start(id)
}

func (m *appManager) stopAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.processes))
	for id, process := range m.processes {
		if process != nil && process.exitCode == nil {
			ids = append(ids, id)
		}
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.stop(id)
	}
}

func (m *appManager) view(id string, extras appExtras) appView {
	m.mu.Lock()
	recipe, ok := m.recipes[id]
	var managed *managedApp
	if ok {
		managed = m.processes[id]
	}
	view := m.viewLocked(recipe, managed, extras)
	m.mu.Unlock()
	return view
}

func (m *appManager) snapshot() []appView { return m.snapshotWithExtras(appExtras{}) }

func (m *appManager) snapshotWithExtras(extras appExtras) []appView {
	m.mu.Lock()
	recipes := make([]appRecipe, 0, len(m.recipes))
	for _, recipe := range m.recipes {
		recipes = append(recipes, recipe)
	}
	sort.Slice(recipes, func(i, j int) bool { return recipes[i].ID < recipes[j].ID })
	views := make([]appView, 0, len(recipes))
	for _, recipe := range recipes {
		views = append(views, m.viewLocked(recipe, m.processes[recipe.ID], extras))
	}
	m.mu.Unlock()
	sort.SliceStable(views, func(i, j int) bool {
		rank := func(view appView) int {
			if view.Running {
				return 0
			}
			if view.Detected {
				return 1
			}
			return 2
		}
		return rank(views[i]) < rank(views[j]) || (rank(views[i]) == rank(views[j]) && views[i].Name < views[j].Name)
	})
	return views
}

func (m *appManager) viewLocked(recipe appRecipe, managed *managedApp, extras appExtras) appView {
	if recipe.ID == "terminal" || recipe.ID == "files" {
		return m.finishView(recipe, appView{Detected: true, Running: recipe.ID == "terminal" && extras.TerminalRunning})
	}
	if recipe.ID == "browser" && extras.BrowserRunning {
		return m.finishView(recipe, appView{Detected: true, Running: true})
	}
	if managed != nil && managed.exitCode == nil {
		return m.finishView(recipe, appView{Detected: true, Running: true, PID: managed.pid, Port: managed.port, ExitCode: managed.exitCode, Message: "Survives a page reload, not a boxdeck restart"})
	}
	port := 0
	if managed != nil {
		port = managed.port
	}
	if port == 0 {
		port = int(recipe.Start.Port)
	}
	if port == 0 {
		port = int(recipe.Detect.Port)
	}
	detected := detectApp(recipe, m.cfg.home)
	running := detected && recipe.Detect.Port > 0
	return m.finishView(recipe, appView{Detected: detected, Running: running, Port: port, ExitCode: func() *int {
		if managed == nil {
			return nil
		}
		return managed.exitCode
	}(), Message: func() string {
		if managed != nil && managed.exitError != "" {
			return managed.exitError
		}
		return ""
	}()})
}

func (m *appManager) finishView(recipe appRecipe, view appView) appView {
	view.ID, view.Name, view.Tag = recipe.ID, recipe.Name, recipe.Tag
	view.Status = "missing"
	if view.Running {
		view.Status = "running"
	} else if view.Detected {
		view.Status = "detected"
	}
	view.InstallHint, view.InstallURL, view.Docs = recipe.Install.Hint, recipe.Install.URL, recipe.Docs
	view.OpenPath, view.Embed = recipe.Open.Path, recipe.Open.Embed
	view.CanStart = len(recipe.Start.Cmd) > 0 || recipe.ID == "browser"
	view.CanStop = view.Running && (len(recipe.Start.Cmd) > 0 || recipe.ID == "browser")
	port := view.Port
	if port == 0 {
		port = int(recipe.Start.Port)
	}
	if port == 0 {
		port = int(recipe.Detect.Port)
	}
	view.Port = port
	if recipe.Open.URL != "" && port > 0 {
		view.URL = expandAppTemplate(recipe.Open.URL, appTemplateValues{Home: m.cfg.home, ID: recipe.ID, Port: port, Host: m.cfg.Host})
	}
	if recipe.Health.HTTP != "" || recipe.Health.Port != 0 {
		view.Health = checkAppHealth(recipe, appRuntime{Port: port})
	}
	if len(view.Log) == 0 {
		view.Log, _ = tailAppLog(filepath.Join(m.cfg.home, ".local", "share", "boxdeck", "apps", recipe.ID+".log"), 20)
	}
	return view
}

func (m *appManager) snapshotLog(id string, lines int) ([]string, error) {
	m.mu.Lock()
	recipe, ok := m.recipes[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("unknown app %q", id)
	}
	if lines < 1 {
		lines = 1
	}
	if lines > 1000 {
		lines = 1000
	}
	return tailAppLog(filepath.Join(m.cfg.home, ".local", "share", "boxdeck", "apps", recipe.ID+".log"), lines)
}

func (a *app) appsAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	extra := appExtras{TerminalRunning: a.terminalStatus().Available}
	if a.collect != nil {
		extra.BrowserRunning = len(browserProcesses(a.collect.getProcesses())) > 0
	}
	jsonReply(w, http.StatusOK, a.apps.snapshotWithExtras(extra))
}

func (a *app) appsReloadAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	err := a.apps.reload()
	extra := appExtras{TerminalRunning: a.terminalStatus().Available}
	views := a.apps.snapshotWithExtras(extra)
	response := object{"apps": views, "reloaded": true}
	if err != nil {
		response["error"] = err.Error()
	}
	jsonReply(w, http.StatusOK, response)
}

func (a *app) appActionAPI(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/apps/"), "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		jsonReply(w, http.StatusNotFound, object{"error": "App action not found"})
		return
	}
	id, err := urlPathPart(parts[0])
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": "Invalid app id"})
		return
	}
	action := parts[1]
	if action == "log" {
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		lines := 20
		if raw := r.URL.Query().Get("lines"); raw != "" {
			lines, err = strconv.Atoi(raw)
			if err != nil || lines < 1 || lines > 1000 {
				jsonReply(w, http.StatusBadRequest, object{"error": "lines must be between 1 and 1000"})
				return
			}
		}
		log, err := a.apps.snapshotLog(id, lines)
		if err != nil {
			jsonReply(w, http.StatusNotFound, object{"error": err.Error()})
			return
		}
		jsonReply(w, http.StatusOK, object{"id": id, "lines": log})
		return
	}
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var view appView
	switch action {
	case "start":
		view, err = a.apps.start(id)
	case "stop":
		err = a.apps.stop(id)
		if err == nil {
			view = a.apps.view(id, appExtras{TerminalRunning: a.terminalStatus().Available})
		}
	case "restart":
		view, err = a.apps.restart(id)
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "Unknown app action"})
		return
	}
	if err != nil {
		status := http.StatusConflict
		if strings.HasPrefix(err.Error(), "unknown app") {
			status = http.StatusNotFound
		}
		jsonReply(w, status, object{"error": err.Error()})
		return
	}
	jsonReply(w, http.StatusOK, view)
}

func urlPathPart(value string) (string, error) {
	decoded, err := url.PathUnescape(value)
	if err != nil || decoded == "" || strings.Contains(decoded, "/") {
		return "", errors.New("invalid path part")
	}
	return decoded, nil
}

func appCommand(argv []string) error {
	if len(argv) == 0 || argv[0] == "list" {
		if len(argv) > 1 {
			return fmt.Errorf("usage: boxdeck app list")
		}
	} else if len(argv) != 2 || (argv[0] != "start" && argv[0] != "stop" && argv[0] != "restart" && argv[0] != "log") {
		return fmt.Errorf("usage: boxdeck app list|start ID|stop ID|restart ID|log ID")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(configPath(home), home)
	if err != nil {
		return err
	}
	if len(argv) == 0 || argv[0] == "list" {
		if data, requestErr := localAppRequest(cfg, http.MethodGet, "/api/apps", nil); requestErr == nil {
			_, err = os.Stdout.Write(data)
			return err
		}
		manager := newAppsManager(context.Background(), cfg)
		return jsonEncode(os.Stdout, manager.snapshot())
	}
	path := "/api/apps/" + url.PathEscape(argv[1]) + "/" + argv[0]
	method := http.MethodPost
	if argv[0] == "log" {
		method = http.MethodGet
		path += "?lines=200"
	}
	data, err := localAppRequest(cfg, method, path, nil)
	if err != nil {
		return err
	}
	_, err = os.Stdout.Write(data)
	return err
}

func localAppRequest(cfg config, method, path string, body []byte) ([]byte, error) {
	base := "http://" + net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port)))
	request, err := http.NewRequest(method, strings.TrimRight(base, "/")+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	request.SetBasicAuth(cfg.User, cfg.Password)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	client := &http.Client{Timeout: 65 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("connect to local boxdeck: %w", err)
	}
	defer response.Body.Close()
	data, err := io.ReadAll(io.LimitReader(response.Body, 16<<20))
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
	}
	return data, nil
}

func tailAppLog(path string, lines int) ([]string, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	parts := strings.Split(strings.TrimRight(string(data), "\r\n"), "\n")
	if len(parts) == 1 && parts[0] == "" {
		return []string{}, nil
	}
	if lines < len(parts) {
		parts = parts[len(parts)-lines:]
	}
	return parts, nil
}

// lookPathWide finds a binary on PATH or in the usual per-user and package manager bin folders,
// because a systemd user service starts with a short PATH that misses ~/.local/bin and friends.
func lookPathWide(name string) (string, error) {
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	home, _ := os.UserHomeDir()
	for _, dir := range []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin"), filepath.Join(home, ".cargo", "bin"), filepath.Join(home, "bin"), "/usr/local/bin", "/opt/homebrew/bin", "/snap/bin"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}
