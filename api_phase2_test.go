package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestBearerTokenAuthenticatesAPI(t *testing.T) {
	a := testApp(t)
	a.cfg.agentRE = regexp.MustCompile(`^$`)
	a.cfg.Tokens = []string{"phase2-secret-token"}
	r := httptest.NewRequest(http.MethodGet, "/api/ports", nil)
	r.Header.Set("Authorization", "Bearer phase2-secret-token")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code == http.StatusUnauthorized {
		t.Fatalf("bearer token was rejected: %s", w.Body.String())
	}
	r = httptest.NewRequest(http.MethodGet, "/api/ports", nil)
	r.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong bearer token got status %d", w.Code)
	}
}

func TestCtlArgsParseGlobalFlagsAndPromptText(t *testing.T) {
	got, err := parseCtlArgs([]string{"--to", "http://box:8100", "--token", "abc", "--table", "prompt", "pane-1", "fix", "the", "build"})
	if err != nil {
		t.Fatal(err)
	}
	if got.To != "http://box:8100" || got.Token != "abc" || !got.Table || got.Command != "prompt" || len(got.Args) != 2 || got.Args[1] != "fix the build" {
		t.Fatalf("parsed ctl args: %+v", got)
	}
}

func TestBoxesFetchInParallelAndRememberFirstFailure(t *testing.T) {
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer remote-token" {
			t.Error("remote token was not kept server-side")
		}
		_ = json.NewEncoder(w).Encode(object{
			"health": object{"cpu": float64(12)},
			"agents": []object{{"pid": float64(9)}},
			"ports":  []object{{"port": float64(3001)}, {"port": float64(8100)}},
		})
	}))
	defer remote.Close()
	a := testApp(t)
	a.cfg.agentRE = regexp.MustCompile(`^$`)
	a.cfg.Host = "local-box"
	a.cfg.Boxes = []boxConfig{
		{Name: "remote", URL: remote.URL, Token: "remote-token"},
		{Name: "offline", URL: "http://127.0.0.1:1", Token: "offline-token"},
	}
	first := a.boxes(context.Background())
	if len(first) != 3 || !first[0].Local || first[0].Name != "local-box" {
		t.Fatalf("local box was not first: %+v", first)
	}
	if !first[1].OK || first[1].Agents != 1 || first[1].Ports != 2 || first[1].Health["cpu"] != float64(12) {
		t.Fatalf("remote box: %+v", first[1])
	}
	if first[2].OK || first[2].Since == "" {
		t.Fatalf("offline box did not expose failure time: %+v", first[2])
	}
	firstFailure := first[2].Since
	second := a.boxes(context.Background())
	if second[2].Since != firstFailure {
		t.Fatalf("failure timestamp changed within cache: %q then %q", firstFailure, second[2].Since)
	}
	if time.Since(parseRFC3339(t, firstFailure)) > 5*time.Second {
		t.Fatalf("failure timestamp is not recent: %s", firstFailure)
	}
}

func parseRFC3339(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("invalid failure timestamp %q: %v", value, err)
	}
	return parsed
}

func TestRunIsDisabledByDefault(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest(http.MethodPost, "/api/run", strings.NewReader(`{"cmd":"printf should-not-run"}`))
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "allowRun") {
		t.Fatalf("run was not refused by default: %d %s", w.Code, w.Body.String())
	}
}
