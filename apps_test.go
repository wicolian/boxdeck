package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestAppRecipesLoadBuiltinsAndHomeOverridesConfig(t *testing.T) {
	home := t.TempDir()
	cfg := appTestConfig(home)
	cfg.Apps = []appRecipe{{ID: "terminal", Name: "Configured terminal"}, {ID: "custom", Name: "Config custom"}}
	appDir := filepath.Join(home, ".config", "boxdeck", "apps")
	if err := os.MkdirAll(appDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "terminal.json"), []byte(`{"id":"terminal","name":"Home terminal","tag":"override"}`), 0600); err != nil {
		t.Fatal(err)
	}
	recipes, err := loadAppRecipes(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if recipes["terminal"].Name != "Home terminal" {
		t.Fatalf("home recipe did not override config recipe: %+v", recipes["terminal"])
	}
	if recipes["custom"].Name != "Config custom" {
		t.Fatalf("config recipe missing: %+v", recipes["custom"])
	}
	if recipes["files"].Name == "" {
		t.Fatal("embedded built-in recipe missing")
	}
}

func TestAppTemplateExpansion(t *testing.T) {
	got := expandAppTemplate("{home}/.cache/{id}/{port}/{host}", appTemplateValues{Home: "/tmp/home", ID: "demo", Port: 4321, Host: "box"})
	if got != "/tmp/home/.cache/demo/4321/box" {
		t.Fatalf("unexpected expansion %q", got)
	}
}

func TestAppDetectionByBinAndPort(t *testing.T) {
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(binDir, "fake-app")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	binRecipe := appRecipe{ID: "bin", Detect: appDetect{Bin: "fake-app"}}
	if !detectApp(binRecipe, home) {
		t.Fatal("bin recipe was not detected")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	portRecipe := appRecipe{ID: "port", Detect: appDetect{Port: portNumber(port)}}
	if !detectApp(portRecipe, home) {
		t.Fatal("port recipe was not detected")
	}
}

func TestAppHealthHTTPAndPort(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	defer server.Close()
	recipe := appRecipe{Health: appHealth{HTTP: server.URL}}
	if !checkAppHealth(recipe, appRuntime{Port: 0}) {
		t.Fatal("healthy HTTP app was reported unhealthy")
	}
	recipe.Health.HTTP = "http://127.0.0.1:1"
	if checkAppHealth(recipe, appRuntime{}) {
		t.Fatal("unreachable HTTP app was reported healthy")
	}
}

func TestManagedAppStartsStopsAndTailsLog(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scratch shell recipe is Unix-only")
	}
	home := t.TempDir()
	script := filepath.Join(home, "fake-app.sh")
	source := "#!/bin/sh\necho ready\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.02; done\n"
	if err := os.WriteFile(script, []byte(source), 0700); err != nil {
		t.Fatal(err)
	}
	cfg := appTestConfig(home)
	m := &appManager{ctx: context.Background(), cfg: cfg, recipes: map[string]appRecipe{
		"fake": {ID: "fake", Name: "Fake", Start: appStart{Cmd: []string{script}, Port: 0}, Stop: appStop{Mode: "signal"}},
	}}
	if _, err := m.start("fake"); err != nil {
		t.Fatal(err)
	}
	defer m.stopAll()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		view := m.snapshot()[0]
		if view.Running && strings.Contains(strings.Join(view.Log, "\n"), "ready") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	view := m.snapshot()[0]
	if !view.Running || view.PID == 0 || !strings.Contains(strings.Join(view.Log, "\n"), "ready") {
		t.Fatalf("managed app did not start with log: %+v", view)
	}
	if err := m.stop("fake"); err != nil {
		t.Fatal(err)
	}
	view = m.snapshot()[0]
	if view.Running || view.ExitCode == nil {
		t.Fatalf("managed app did not stop: %+v", view)
	}
}

func TestT3RecipeStartsFakeT3FromPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("scratch shell recipe is Unix-only")
	}
	home := t.TempDir()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0700); err != nil {
		t.Fatal(err)
	}
	fake := filepath.Join(binDir, "t3")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\necho fake-t3-ready\ntrap 'exit 0' TERM INT\nwhile :; do sleep 0.02; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg := appTestConfig(home)
	m := &appManager{ctx: context.Background(), cfg: cfg, recipes: map[string]appRecipe{
		"t3code": {ID: "t3code", Name: "T3 Code", Start: appStart{Cmd: []string{"t3", "serve", "--port", "{port}"}, Port: 0}, Stop: appStop{Mode: "signal"}},
	}}
	if _, err := m.start("t3code"); err != nil {
		t.Fatal(err)
	}
	defer m.stopAll()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(strings.Join(m.snapshot()[0].Log, "\n"), "fake-t3-ready") {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(strings.Join(m.snapshot()[0].Log, "\n"), "fake-t3-ready") {
		t.Fatal("fake t3 did not run from PATH")
	}
	if err := m.stop("t3code"); err != nil {
		t.Fatal(err)
	}
}

func TestAppLogTailLimitsLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("one\ntwo\nthree\nfour\n"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := tailAppLog(path, 2)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "three|four" {
		t.Fatalf("unexpected tail %v", got)
	}
}

func TestAppsAPIListsRecipesAndServesEmbeddedAssets(t *testing.T) {
	a := testApp(t)
	response := request(a, http.MethodGet, "/api/apps", true)
	if response.Code != http.StatusOK {
		t.Fatalf("apps API status: %d %s", response.Code, response.Body.String())
	}
	var recipes []appView
	if err := json.Unmarshal(response.Body.Bytes(), &recipes); err != nil {
		t.Fatal(err)
	}
	if len(recipes) < 10 {
		t.Fatalf("expected built-in recipes, got %d", len(recipes))
	}
	asset := request(a, http.MethodGet, "/assets/views-apps.js", true)
	if asset.Code != http.StatusOK || !strings.Contains(asset.Body.String(), "addView") {
		t.Fatalf("embedded app JS unavailable: %d", asset.Code)
	}
	if got := request(a, http.MethodPost, "/api/apps", true).Code; got != http.StatusMethodNotAllowed {
		t.Fatalf("apps list accepted POST: %d", got)
	}
}

func TestCtlAppCommands(t *testing.T) {
	args, err := parseCtlArgs([]string{"apps"})
	if err != nil || args.Command != "apps" {
		t.Fatalf("ctl apps parse: %+v, %v", args, err)
	}
	args, err = parseCtlArgs([]string{"app", "start", "t3code"})
	if err != nil || args.Command != "app" || strings.Join(args.Args, "/") != "start/t3code" {
		t.Fatalf("ctl app parse: %+v, %v", args, err)
	}
	method, path, _, err := ctlRequest(args)
	if err != nil || method != http.MethodPost || path != "/api/apps/t3code/start" {
		t.Fatalf("ctl app request: %s %s, %v", method, path, err)
	}
}

func appTestConfig(home string) config {
	return config{home: home, Host: "box", Bind: "127.0.0.1", Port: 8100, User: "admin", Password: "password", FilesRoot: home}
}
