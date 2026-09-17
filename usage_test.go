package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestUsageScannerRollsUpClaudeAndCodexSessions(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "claude", "projects", "demo")
	codex := filepath.Join(root, "codex", "sessions", "2026", "09", "17")
	if err := os.MkdirAll(claude, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(codex, 0o755); err != nil {
		t.Fatal(err)
	}
	copyFixture(t, "testdata/usage/claude-session.jsonl", filepath.Join(claude, "session.jsonl"))
	copyFixture(t, "testdata/usage/codex-session.jsonl", filepath.Join(codex, "rollout.jsonl"))

	store, err := scanUsageRoots(filepath.Join(root, "claude"), filepath.Join(root, "codex"), pricingConfig{}, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(store.Files) != 2 {
		t.Fatalf("files = %d, want 2", len(store.Files))
	}
	day := store.Days["claude|2026-09-17"]
	if day.Tokens.In != 10 || day.Tokens.Out != 20 || day.Sessions != 1 {
		t.Fatalf("claude day = %+v", day)
	}
	if day := store.Days["claude|2026-09-16"]; day.Tokens.In != 1200 || day.Tokens.CachedIn != 330 || day.Tokens.CacheWrite != 220 || day.Tokens.Out != 450 {
		t.Fatalf("deduped claude day = %+v", day)
	}
	codexDay := store.Days["codex|2026-09-17"]
	if codexDay.Tokens.In != 500 || codexDay.Tokens.CachedIn != 100 || codexDay.Tokens.CacheWrite != 25 || codexDay.Tokens.Out != 200 || codexDay.Sessions != 1 {
		t.Fatalf("codex day = %+v", codexDay)
	}
	if store.CodexQuota.Primary.Pct != 42 || store.CodexQuota.Secondary.Pct != 11 {
		t.Fatalf("codex quota = %+v", store.CodexQuota)
	}
}

func TestUsageScannerReplacesGrowingFileWithoutDoubleCounting(t *testing.T) {
	root := t.TempDir()
	claude := filepath.Join(root, "claude")
	codex := filepath.Join(root, "codex")
	path := filepath.Join(codex, "sessions", "rollout.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	copyFixture(t, "testdata/usage/codex-growing.jsonl", path)
	first, err := scanUsageRoots(claude, codex, pricingConfig{}, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if got := first.Days["codex|2026-09-17"].Tokens; got.In != 7 || got.CachedIn != 2 || got.Out != 3 {
		t.Fatalf("first scan = %+v", got)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString("{\"timestamp\":\"2026-09-17T02:02:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"total_token_usage\":{\"input_tokens\":17,\"cached_input_tokens\":4,\"output_tokens\":9}}}}\n")
	if closeErr := f.Close(); err != nil || closeErr != nil {
		t.Fatalf("append: %v %v", err, closeErr)
	}
	second, err := scanUsageRoots(claude, codex, pricingConfig{}, time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	got := second.Days["codex|2026-09-17"].Tokens
	if got.In != 17 || got.CachedIn != 4 || got.Out != 9 {
		t.Fatalf("growing scan = %+v", got)
	}
}

func TestUsagePersistenceIsAtomicAndPrivate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usage.json")
	store := usageStore{Version: 1, Days: map[string]usageDay{"claude|2026-09-17": {Sessions: 1}}}
	if err := saveUsageStore(path, store); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
	var decoded usageStore
	if b, err := os.ReadFile(path); err != nil || json.Unmarshal(b, &decoded) != nil || decoded.Version != 1 {
		t.Fatalf("saved store was not readable: %s", string(mustRead(t, path)))
	}
}

func TestUsagePricingOverrideAndUnknownPrice(t *testing.T) {
	override := pricingConfig{Models: map[string]pricing{"custom": {In: 1, CachedIn: 0.5, CacheWrite: 2, Out: 3}}}
	got := pricingFor("custom", override)
	if got.In != 1 || got.CachedIn != 0.5 || got.CacheWrite != 2 || got.Out != 3 {
		t.Fatalf("override = %+v", got)
	}
	if pricingFor("not-listed", pricingConfig{}).Set {
		t.Fatal("unknown model unexpectedly has a price")
	}
}

func TestUsageQuotaParsingPreservesNullModelWindows(t *testing.T) {
	var got claudeQuotaResponse
	if err := json.Unmarshal([]byte(`{"five_hour":{"utilization":20,"resets_at":"2026-09-17T13:00:00Z"},"seven_day":{"utilization":68,"resets_at":"2026-09-24T00:00:00Z"},"seven_day_opus":null,"seven_day_sonnet":{"utilization":40,"resets_at":"2026-09-24T00:00:00Z"}}`), &got); err != nil {
		t.Fatal(err)
	}
	if got.FiveHour.Pct != 20 || got.SevenDay.Pct != 68 || got.Opus != nil || got.Sonnet == nil || got.Sonnet.Pct != 40 {
		t.Fatalf("quota = %+v", got)
	}
}

func TestUsageSnapshotLimitsDaysAndIncludesLocalDay(t *testing.T) {
	store := usageStore{Days: map[string]usageDay{
		"claude|2026-09-15": {Provider: "claude", Day: "2026-09-15", Tokens: tokenTotals{In: 1}},
		"claude|2026-09-16": {Provider: "claude", Day: "2026-09-16", Tokens: tokenTotals{In: 2}},
	}, Files: map[string]usageFile{}, UpdatedAt: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)}
	response := snapshotFromStore(store, 1, time.Date(2026, 9, 16, 12, 0, 0, 0, time.FixedZone("local", 2*60*60)))
	if response.Device != "box" || len(response.Providers["claude"].Days) != 1 || response.Providers["claude"].LocalDay == "" {
		t.Fatalf("snapshot = %+v", response)
	}
}

func TestUsageAPIIsAuthenticatedAndClampsDays(t *testing.T) {
	a := testApp(t)
	a.cancel()
	a.usage.mu.Lock()
	a.usage.store = usageStore{Version: 1, UpdatedAt: time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC), Days: map[string]usageDay{
		"claude|2026-09-17": {Provider: "claude", Day: "2026-09-17", Tokens: tokenTotals{In: 3}},
	}, Files: map[string]usageFile{}}
	a.usage.mu.Unlock()
	unauthenticated := request(a, http.MethodGet, "/api/usage?days=999", false)
	if unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status = %d", unauthenticated.Code)
	}
	authenticated := request(a, http.MethodGet, "/api/usage?days=999", true)
	if authenticated.Code != http.StatusOK || !strings.Contains(authenticated.Body.String(), `"device":"box"`) {
		t.Fatalf("usage response = %d %s", authenticated.Code, authenticated.Body.String())
	}
	method := httptest.NewRequest(http.MethodPost, "/api/usage", nil)
	method.SetBasicAuth(a.cfg.User, a.cfg.Password)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, method)
	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("usage POST status = %d", w.Code)
	}
}

func copyFixture(t *testing.T, source, target string) {
	t.Helper()
	b, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
