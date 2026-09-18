package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestActionTokenIsBoundToOneAlert(t *testing.T) {
	a := testApp(t)
	a.alerts.mintAction = func(id string) string { return a.mintActionToken(id, time.Now()) }
	alert, _ := a.alerts.raise(Alert{Rule: "agent_needs_you", Severity: "warning", Title: "Approve?", Actions: []AlertAction{{Label: "Approve", Method: "POST", Path: "/api/herd/keys", Body: map[string]any{"pane": "w1:p1", "keys": "Enter"}}}})
	token := a.mintActionToken(alert.ID, time.Now())
	try := func(method, path, tok string) bool {
		r := httptest.NewRequest(method, path, nil)
		r.Header.Set("Authorization", "Bearer "+tok)
		return a.tokenAuthed(r)
	}
	if !try(http.MethodPost, "/api/alerts/"+alert.ID+"/ack", token) {
		t.Fatal("ack with a valid action token must pass")
	}
	if !try(http.MethodPost, "/api/herd/keys", token) {
		t.Fatal("the alert's own action must pass")
	}
	if try(http.MethodPost, "/api/proc/kill", token) {
		t.Fatal("a path the alert does not list must fail")
	}
	if try(http.MethodGet, "/api/state", token) {
		t.Fatal("reading state with an action token must fail")
	}
	if try(http.MethodPost, "/api/alerts/"+alert.ID+"/ack", a.mintActionToken(alert.ID, time.Now().Add(-48*time.Hour))) {
		t.Fatal("an expired token must fail")
	}
	if try(http.MethodPost, "/api/alerts/"+alert.ID+"/ack", token[:len(token)-2]+"xx") {
		t.Fatal("a tampered signature must fail")
	}
}
