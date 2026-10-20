package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func bearerRequest(a *app, method, target, token, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer "+token)
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func TestReadOnlyTokenOnlyReadsTheAllowlist(t *testing.T) {
	a := testApp(t)
	a.cfg.AllowRun = true
	full, _ := randomToken()
	readOnly, _ := randomPrefixedToken(readOnlyTokenPrefix)
	a.cfg.Tokens = []string{full}
	a.cfg.ReadOnlyTokens = []string{readOnly}

	for _, path := range []string{"/api/boxes", "/api/agents", "/api/usage", "/api/usage/all", "/api/health"} {
		if w := bearerRequest(a, http.MethodGet, path, readOnly, ""); w.Code != http.StatusOK {
			t.Errorf("GET %s with a read-only token: %d, want 200", path, w.Code)
		}
	}
	if w := bearerRequest(a, http.MethodHead, "/api/boxes", readOnly, ""); w.Code == http.StatusForbidden || w.Code == http.StatusUnauthorized {
		t.Errorf("HEAD /api/boxes with a read-only token: %d", w.Code)
	}

	denied := []struct{ method, path, body string }{
		{http.MethodPost, "/api/run", `{"cmd":"true"}`},
		{http.MethodGet, "/api/run", ""},
		{http.MethodPost, "/api/boxes", `{}`},
		{http.MethodPut, "/api/boxes", `{}`},
		{http.MethodDelete, "/api/boxes", ""},
		{http.MethodPost, "/api/usage", `{}`},
		{http.MethodGet, "/api/state", ""},
		{http.MethodGet, "/api/procs", ""},
		{http.MethodGet, "/api/files", ""},
		{http.MethodPost, "/api/proc/kill", `{}`},
		{http.MethodGet, "/api/pair.json", ""},
		{http.MethodGet, "/api/pair", ""},
		{http.MethodPost, "/api/connect", `{}`},
		{http.MethodGet, "/api/alerts", ""},
		{http.MethodPost, "/api/alerts/x/ack", ""},
		{http.MethodGet, "/api/stream", ""},
		{http.MethodGet, "/cdp", ""},
		{http.MethodGet, "/mcp", ""},
		{http.MethodPost, "/mcp", `{}`},
		{http.MethodGet, "/", ""},
		{http.MethodGet, "/files/", ""},
		{http.MethodGet, "/term/", ""},
		{http.MethodGet, "/login", ""},
		{http.MethodGet, "/api/boxes/", ""},
	}
	for _, d := range denied {
		w := bearerRequest(a, d.method, d.path, readOnly, d.body)
		if w.Code != http.StatusForbidden {
			t.Errorf("%s %s with a read-only token: %d, want 403", d.method, d.path, w.Code)
		}
	}

	// A websocket upgrade on an allowlisted path is still refused.
	r := httptest.NewRequest(http.MethodGet, "/api/agents", nil)
	r.Header.Set("Authorization", "Bearer "+readOnly)
	r.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("websocket upgrade with a read-only token: %d, want 403", w.Code)
	}

	// The read-only token is never accepted as a ?token= on /cdp.
	if w := request(a, http.MethodGet, "/cdp?token="+readOnly, false); w.Code == http.StatusOK {
		t.Errorf("/cdp?token= with a read-only token: %d", w.Code)
	}
}

func TestFullTokenStillReachesRun(t *testing.T) {
	a := testApp(t)
	a.cfg.AllowRun = true
	full, _ := randomToken()
	readOnly, _ := randomPrefixedToken(readOnlyTokenPrefix)
	a.cfg.Tokens = []string{full}
	a.cfg.ReadOnlyTokens = []string{readOnly}
	w := bearerRequest(a, http.MethodPost, "/api/run", full, `{"cmd":"printf ok"}`)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("POST /api/run with a full token: %d %s", w.Code, w.Body.String())
	}
	if w := bearerRequest(a, http.MethodGet, "/api/state", full, ""); w.Code != http.StatusOK {
		t.Fatalf("GET /api/state with a full token: %d", w.Code)
	}
}

func TestReadOnlyTokenCommand(t *testing.T) {
	a := testApp(t)
	cfg := &a.cfg
	stdout := os.Stdout
	read, write, _ := os.Pipe()
	os.Stdout = write
	err := tokenCommandWithConfig(cfg, []string{"new", "--read-only", "tv"})
	os.Stdout = stdout
	write.Close()
	if err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 256)
	n, _ := read.Read(buf)
	token := strings.TrimSpace(string(buf[:n]))
	if !strings.HasPrefix(token, readOnlyTokenPrefix) || len(token) != len(readOnlyTokenPrefix)+64 {
		t.Fatalf("read-only token has the wrong shape: %d characters", len(token))
	}
	if len(cfg.Tokens) != 0 || len(cfg.ReadOnlyTokens) != 1 || tokenLabel(*cfg, token) != "tv" {
		t.Fatal("a read-only token must be stored only in readOnlyTokens, with its label")
	}
	b, _ := os.ReadFile(cfg.path)
	var saved map[string]any
	if json.Unmarshal(b, &saved) != nil || saved["readOnlyTokens"] == nil {
		t.Fatal("readOnlyTokens must be saved to the config file")
	}
	if err := tokenCommandWithConfig(cfg, []string{"new", "--bogus"}); err == nil {
		t.Fatal("an unknown flag must fail")
	}
	if err := tokenCommandWithConfig(cfg, []string{"revoke", token[:12]}); err != nil {
		t.Fatal(err)
	}
	if len(cfg.ReadOnlyTokens) != 0 || tokenLabel(*cfg, token) != "" {
		t.Fatal("revoke must remove a read-only token and its label")
	}
}

func TestOldConfigWithoutReadOnlyTokensLoads(t *testing.T) {
	home := t.TempDir()
	path := home + "/config.json"
	if err := os.WriteFile(path, []byte(`{"password":"long:password","tokens":["bd_old"],"tokenLabels":{"bd_old":"agent"}}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := loadConfig(path, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Tokens) != 1 || len(cfg.ReadOnlyTokens) != 0 {
		t.Fatal("an old config must load unchanged")
	}
}
