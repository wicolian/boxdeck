package main

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func alertTestManager(t *testing.T) *alertManager {
	t.Helper()
	now := time.Date(2026, 9, 17, 12, 0, 0, 0, time.UTC)
	m := newAlertManagerAt(alertConfig{}, filepath.Join(t.TempDir(), "alerts.json"))
	m.now = func() time.Time { return now }
	return m
}

func TestAlertRulesNeedsYouStuckAndDedupe(t *testing.T) {
	m := alertTestManager(t)
	m.cfg.Rules = defaultAlertRules()
	m.cfg.Rules.StuckMinutes = 1
	m.now = func() time.Time { return time.Date(2026, 9, 17, 12, 2, 0, 0, time.UTC) }
	agent := agentInfo{Kind: "claude", Pane: "qa:0.0", PaneID: "qa-pane", Status: "working"}
	m.evaluate(alertSnapshot{Agents: []agentInfo{agent}, Tails: map[string]string{"qa:0.0": "Allow? (y/n)"}})
	alerts := m.list("open", "", "", "")
	if len(alerts) != 1 || alerts[0].Rule != "agent_needs_you" {
		t.Fatalf("needs-you alerts = %+v", alerts)
	}
	m.evaluate(alertSnapshot{Agents: []agentInfo{agent}, Tails: map[string]string{"qa:0.0": "same output"}})
	m.now = func() time.Time { return time.Date(2026, 9, 17, 12, 3, 1, 0, time.UTC) }
	m.evaluate(alertSnapshot{Agents: []agentInfo{agent}, Tails: map[string]string{"qa:0.0": "same output"}})
	alerts = m.list("open", "", "", "")
	if len(alerts) != 2 {
		t.Fatalf("expected needs-you and stuck alerts, got %+v", alerts)
	}
	first, _ := m.raise(Alert{Box: "box", Rule: "probe_failed", Subject: "ci", Severity: "critical", Title: "CI failed", Body: "failure"})
	second, _ := m.raise(Alert{Box: "box", Rule: "probe_failed", Subject: "ci", Severity: "critical", Title: "CI failed again", Body: "failure again"})
	if first.ID != second.ID || second.Count != 2 {
		t.Fatalf("dedupe = first %+v second %+v", first, second)
	}
}

func TestAlertQuietSnoozeAndPersistence(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "alerts.json")
	cfg := alertConfig{Quiet: alertQuiet{From: "23:00", To: "08:00"}, QuietAllowCritical: false}
	m := newAlertManagerAt(cfg, path)
	now := time.Date(2026, 9, 17, 23, 30, 0, 0, time.UTC)
	m.now = func() time.Time { return now }
	alert, delivered := m.raise(Alert{Box: "box", Rule: "probe_failed", Subject: "ci", Severity: "warning", Title: "CI failed", Body: "failure"})
	if delivered {
		t.Fatal("quiet-hour warning was delivered")
	}
	if err := m.snooze(alert.ID, now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := m.list("snoozed", "", "", ""); len(got) != 1 {
		t.Fatalf("snoozed alerts = %+v", got)
	}
	m2 := newAlertManagerAt(cfg, path)
	if got := m2.list("snoozed", "", "", ""); len(got) != 1 || got[0].ID != alert.ID {
		t.Fatalf("persisted alert = %+v", got)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0600 {
		t.Fatalf("alerts mode = %o", info.Mode().Perm())
	}
}

func TestAlertEventsResumeFromLastID(t *testing.T) {
	m := alertTestManager(t)
	created, _ := m.raise(Alert{Box: "box", Rule: "manual", Subject: "one", Severity: "info", Title: "One", Body: "body"})
	if _, err := m.ack(created.ID); err != nil {
		t.Fatal(err)
	}
	events := m.eventsAfter(0)
	if len(events) < 2 || events[0].ID >= events[1].ID {
		t.Fatalf("events = %+v", events)
	}
	ch, _, stop := m.subscribe(events[0].ID)
	defer stop()
	created2, _ := m.raise(Alert{Box: "box", Rule: "manual", Subject: "two", Severity: "info", Title: "Two", Body: "body"})
	select {
	case event := <-ch:
		if event.Alert == nil || event.Alert.ID != created2.ID {
			t.Fatalf("live event = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestNtfyRequestShapeAndWebhookSignature(t *testing.T) {
	var mu sync.Mutex
	var ntfyReq *http.Request
	var ntfyBody string
	var webhookReq *http.Request
	var webhookBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		defer mu.Unlock()
		if r.Header.Get("Title") != "" {
			copy := r.Clone(r.Context())
			ntfyReq, ntfyBody = copy, string(body)
		} else {
			copy := r.Clone(r.Context())
			webhookReq, webhookBody = copy, string(body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	m := alertTestManager(t)
	m.cfg.Sinks = []alertSink{{Name: "ntfy", Type: "ntfy", URL: server.URL, Topic: "qa", Token: "secret"}, {Name: "hook", Type: "webhook", URL: server.URL + "/hook", Secret: "webhook-secret"}}
	alert := Alert{ID: "a1", Box: "box", Rule: "agent_needs_you", Subject: "pane", Severity: "critical", Title: "Needs you", Body: "Approve it", Link: "#/agents?pane=qa", Actions: agentActions("qa")}
	if err := m.deliverOne(context.Background(), m.cfg.Sinks[0], alert); err != nil {
		t.Fatal(err)
	}
	if err := m.deliverOne(context.Background(), m.cfg.Sinks[1], alert); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if ntfyReq == nil || ntfyReq.Header.Get("Title") != "Needs you" || ntfyReq.Header.Get("Priority") != "max" || !strings.Contains(ntfyReq.Header.Get("Actions"), "Approve") || ntfyBody == "" {
		t.Fatalf("ntfy request = %#v body=%q", ntfyReq, ntfyBody)
	}
	sum := hmac.New(sha256.New, []byte("webhook-secret"))
	_, _ = sum.Write([]byte(webhookBody))
	want := "sha256=" + hex.EncodeToString(sum.Sum(nil))
	if webhookReq == nil || webhookReq.Header.Get("X-Boxdeck-Signature") != want {
		t.Fatalf("webhook signature = %q want %q", webhookReq.Header.Get("X-Boxdeck-Signature"), want)
	}
}

func TestAlertInboxLoopbackHeaderRule(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest(http.MethodPost, "/api/alerts", strings.NewReader(`{"source":"tests","severity":"critical","title":"Tests failed","body":"broken"}`))
	r.RemoteAddr = "127.0.0.1:54321"
	r.Header.Set("X-Boxdeck-Local", "1")
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("loopback inbox status = %d %s", w.Code, w.Body.String())
	}
	remote := httptest.NewRequest(http.MethodPost, "/api/alerts", strings.NewReader(`{"title":"bad"}`))
	remote.RemoteAddr = "192.0.2.1:54321"
	remote.Header.Set("X-Boxdeck-Local", "1")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, remote)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("remote loopback shortcut status = %d", w.Code)
	}
}

func TestAlertsAPIListActionsAndRules(t *testing.T) {
	a := testApp(t)
	create := httptest.NewRequest(http.MethodPost, "/api/alerts", strings.NewReader(`{"source":"tests","severity":"warning","title":"Needs review","body":"open"}`))
	create.SetBasicAuth(a.cfg.User, a.cfg.Password)
	create.Header.Set("Content-Type", "application/json")
	created := httptest.NewRecorder()
	a.ServeHTTP(created, create)
	if created.Code != http.StatusCreated {
		t.Fatalf("create status = %d %s", created.Code, created.Body.String())
	}
	var alert Alert
	if err := json.Unmarshal(created.Body.Bytes(), &alert); err != nil {
		t.Fatal(err)
	}
	list := request(a, http.MethodGet, "/api/alerts?state=open", true)
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), alert.ID) {
		t.Fatalf("list = %d %s", list.Code, list.Body.String())
	}
	ack := request(a, http.MethodPost, "/api/alerts/"+alert.ID+"/ack", true)
	if ack.Code != http.StatusOK || !strings.Contains(ack.Body.String(), `"acked"`) {
		t.Fatalf("ack = %d %s", ack.Code, ack.Body.String())
	}
	rules := httptest.NewRequest(http.MethodPut, "/api/alerts/rules", strings.NewReader(`{"rules":{"agent_stuck":false,"stuckMinutes":1}}`))
	rules.SetBasicAuth(a.cfg.User, a.cfg.Password)
	rules.Header.Set("Content-Type", "application/json")
	rulesResponse := httptest.NewRecorder()
	a.ServeHTTP(rulesResponse, rules)
	if rulesResponse.Code != http.StatusOK || strings.Contains(rulesResponse.Body.String(), `"agent_stuck":true`) {
		t.Fatalf("rules = %d %s", rulesResponse.Code, rulesResponse.Body.String())
	}
}

func TestProbeFailureAndRecovery(t *testing.T) {
	m := alertTestManager(t)
	probe := alertProbe{Name: "bad", Cmd: "exit 7", Every: time.Minute, Timeout: time.Second}
	if err := m.runProbe(context.Background(), probe); err == nil {
		t.Fatal("healthy probe unexpectedly passed")
	}
	m.recordProbeResult(probe, true, "exit 7")
	m.recordProbeResult(probe, false, "")
	alerts := m.list("", "", "", "probe_failed")
	if len(alerts) != 2 || alerts[0].Severity != "info" || alerts[1].Severity != "critical" {
		t.Fatalf("probe lifecycle = %+v", alerts)
	}
}

func TestAlertJSONShape(t *testing.T) {
	m := alertTestManager(t)
	alert, _ := m.raise(Alert{Box: "box", Rule: "manual", Subject: "s", Severity: "info", Title: "Title", Body: "Body", Link: "#/boxes"})
	b, err := json.Marshal(alert)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"id", "box", "rule", "severity", "title", "body", "at", "state", "link", "actions"} {
		if !strings.Contains(string(b), `"`+field+`"`) {
			t.Fatalf("alert JSON missing %s: %s", field, b)
		}
	}
}
