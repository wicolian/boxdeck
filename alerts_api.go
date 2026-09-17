package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func (a *app) alertsAPI(w http.ResponseWriter, r *http.Request) {
	if a.alerts == nil {
		jsonReply(w, http.StatusServiceUnavailable, object{"error": "alerts are not available"})
		return
	}
	switch r.URL.Path {
	case "/api/alerts":
		if r.Method == http.MethodGet {
			jsonReply(w, http.StatusOK, a.alerts.list(r.URL.Query().Get("state"), r.URL.Query().Get("since"), r.URL.Query().Get("box"), r.URL.Query().Get("rule")))
			return
		}
		if r.Method == http.MethodPost {
			var body struct {
				Source   string        `json:"source"`
				Box      string        `json:"box"`
				Rule     string        `json:"rule"`
				Subject  string        `json:"subject"`
				Severity string        `json:"severity"`
				Title    string        `json:"title"`
				Body     string        `json:"body"`
				Link     string        `json:"link"`
				Actions  []AlertAction `json:"actions"`
			}
			if !decodeJSONBody(w, r, &body, 256<<10) {
				return
			}
			if strings.TrimSpace(body.Title) == "" {
				jsonReply(w, http.StatusBadRequest, object{"error": "title is required"})
				return
			}
			if body.Rule == "" {
				body.Rule = strings.TrimSpace(body.Source)
				if body.Rule == "" {
					body.Rule = "manual"
				}
			}
			if body.Box == "" {
				body.Box = a.cfg.Host
			}
			alert, _ := a.alerts.raise(Alert{Box: body.Box, Rule: body.Rule, Subject: body.Subject, Severity: body.Severity, Title: body.Title, Body: body.Body, Link: body.Link, Actions: body.Actions})
			jsonReply(w, http.StatusCreated, alert)
			return
		}
		w.Header().Set("Allow", "GET, POST")
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET or POST for alerts"})
	case "/api/alerts/rules":
		a.alertRulesAPI(w, r)
	case "/api/alerts/snooze-all":
		if r.Method != http.MethodPost {
			requireMethod(w, r, http.MethodPost)
			return
		}
		until, err := alertUntilBody(r, a.alerts.now().Add(2*time.Hour))
		if err != nil {
			jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
			return
		}
		jsonReply(w, http.StatusOK, object{"snoozed": a.alerts.snoozeAll(until), "until": until})
	case "/api/alerts/disarm":
		if r.Method != http.MethodPost {
			requireMethod(w, r, http.MethodPost)
			return
		}
		var body struct {
			On bool `json:"on"`
		}
		if !decodeJSONBody(w, r, &body, 4096) {
			return
		}
		a.alerts.setDisarmed(body.On)
		jsonReply(w, http.StatusOK, object{"disarmed": body.On})
	case "/api/alerts/sinks/test":
		if r.Method != http.MethodGet {
			requireMethod(w, r, http.MethodGet)
			return
		}
		name := r.URL.Query().Get("name")
		sink, ok := a.alertSink(name)
		if !ok {
			jsonReply(w, http.StatusNotFound, object{"error": "sink not found"})
			return
		}
		alert := Alert{ID: "test", Box: a.cfg.Host, Rule: "test", Subject: "test", Severity: "info", Title: "Boxdeck test alert", Body: "Delivery is connected.", Link: "#/alerts", Actions: []AlertAction{{Label: "Snooze 2h", Method: http.MethodPost, Path: "/api/alerts/snooze-all", Body: object{"until": "2h"}}}}
		if err := a.alerts.deliverOne(r.Context(), sink, alert); err != nil {
			jsonReply(w, http.StatusBadGateway, object{"error": err.Error(), "sink": sink.Name})
			return
		}
		jsonReply(w, http.StatusOK, object{"ok": true, "sink": sink.Name})
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "alert route not found"})
	}
}

func (a *app) alertActionAPI(w http.ResponseWriter, r *http.Request) {
	if a.alerts == nil {
		jsonReply(w, http.StatusServiceUnavailable, object{"error": "alerts are not available"})
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/alerts/"), "/")
	if len(parts) == 1 && r.Method == http.MethodGet {
		alert, ok := a.alerts.get(parts[0])
		if !ok {
			jsonReply(w, http.StatusNotFound, object{"error": "alert not found"})
			return
		}
		jsonReply(w, http.StatusOK, alert)
		return
	}
	if len(parts) != 2 || r.Method != http.MethodPost {
		jsonReply(w, http.StatusNotFound, object{"error": "alert action not found"})
		return
	}
	id, action := parts[0], parts[1]
	switch action {
	case "ack":
		alert, err := a.alerts.ack(id)
		if err != nil {
			alertNotFound(w, err)
			return
		}
		jsonReply(w, http.StatusOK, alert)
	case "resolve":
		alert, err := a.alerts.resolve(id)
		if err != nil {
			alertNotFound(w, err)
			return
		}
		jsonReply(w, http.StatusOK, alert)
	case "snooze":
		until, err := alertUntilBody(r, a.alerts.now().Add(2*time.Hour))
		if err != nil {
			jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
			return
		}
		if err := a.alerts.snooze(id, until); err != nil {
			alertNotFound(w, err)
			return
		}
		alert, _ := a.alerts.get(id)
		jsonReply(w, http.StatusOK, alert)
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "unknown alert action"})
	}
}

func alertNotFound(w http.ResponseWriter, err error) {
	if errors.Is(err, os.ErrNotExist) {
		jsonReply(w, http.StatusNotFound, object{"error": "alert not found"})
		return
	}
	jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
}

func alertUntilBody(r *http.Request, fallback time.Time) (time.Time, error) {
	var body struct {
		Until string `json:"until"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		data, err := io.ReadAll(io.LimitReader(r.Body, 4096))
		if err != nil {
			return time.Time{}, err
		}
		if len(strings.TrimSpace(string(data))) > 0 {
			if err := json.Unmarshal(data, &body); err != nil {
				return time.Time{}, errors.New("invalid JSON body")
			}
		}
	}
	if strings.TrimSpace(body.Until) == "" {
		return fallback, nil
	}
	if parsed, err := time.Parse(time.RFC3339, body.Until); err == nil {
		return parsed, nil
	}
	duration, err := time.ParseDuration(body.Until)
	if err != nil {
		return time.Time{}, errors.New("until must be RFC3339 or a duration such as 2h")
	}
	return time.Now().Add(duration), nil
}

func (a *app) alertRulesAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		a.alerts.mu.Lock()
		cfg, disarmed, deliveries := a.alerts.cfg, a.alerts.disarmed, map[string]alertDelivery{}
		for name, value := range a.alerts.delivery {
			deliveries[name] = value
		}
		a.alerts.mu.Unlock()
		sinks := make([]object, 0, len(cfg.Sinks))
		for _, sink := range cfg.Sinks {
			sinks = append(sinks, object{"name": sink.Name, "type": sink.Type, "minSeverity": sink.MinSeverity, "rules": sink.Rules})
		}
		jsonReply(w, http.StatusOK, object{"rules": cfg.Rules, "probes": cfg.Probes, "quiet": cfg.Quiet, "quietAllowCritical": cfg.QuietAllowCritical, "sinks": sinks, "delivery": deliveries, "disarmed": disarmed})
		return
	}
	if r.Method != http.MethodPut {
		requireMethod(w, r, http.MethodPut)
		return
	}
	var body struct {
		Rules              alertRules   `json:"rules"`
		Probes             []alertProbe `json:"probes"`
		Quiet              alertQuiet   `json:"quiet"`
		QuietAllowCritical bool         `json:"quietAllowCritical"`
		Sinks              []alertSink  `json:"sinks"`
	}
	if !decodeJSONBody(w, r, &body, 256<<10) {
		return
	}
	if body.Rules == (alertRules{}) {
		body.Rules = defaultAlertRules()
	}
	a.alerts.mu.Lock()
	cfg := a.alerts.cfg
	cfg.Rules, cfg.Probes, cfg.Quiet = body.Rules, body.Probes, body.Quiet
	cfg.QuietAllowCritical = body.QuietAllowCritical
	if body.Sinks != nil {
		cfg.Sinks = body.Sinks
	}
	a.alerts.cfg = cfg
	a.alerts.mu.Unlock()
	if err := a.persistAlertConfig(cfg); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
		return
	}
	r.Method = http.MethodGet
	a.alertRulesAPI(w, r)
}

func (a *app) alertSink(name string) (alertSink, bool) {
	a.alerts.mu.Lock()
	defer a.alerts.mu.Unlock()
	for _, sink := range a.alerts.cfg.Sinks {
		if name == "" || sink.Name == name {
			return sink, true
		}
	}
	return alertSink{}, false
}

func (a *app) persistAlertConfig(alerts alertConfig) error {
	cfg := a.cfg
	cfg.Alerts = alerts
	cfg.Alerts.RulesComplete = true
	cfg.AlertRules, cfg.Probes, cfg.Quiet = alerts.Rules, alerts.Probes, alerts.Quiet
	cfg.AlertRulesComplete = true
	cfg.QuietAllowCritical, cfg.Sinks = alerts.QuietAllowCritical, alerts.Sinks
	cfg.WatchPIDs, cfg.WatchApps = alerts.WatchPIDs, alerts.WatchApps
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if cfg.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(cfg.path), 0700); err != nil {
		return err
	}
	tmp := cfg.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, cfg.path)
}

func (a *app) alertEventsAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		requireMethod(w, r, http.MethodGet)
		return
	}
	last, _ := strconv.ParseUint(r.Header.Get("Last-Event-ID"), 10, 64)
	if last == 0 {
		last, _ = strconv.ParseUint(r.URL.Query().Get("since"), 10, 64)
	}
	ch, backlog, stop := a.alerts.subscribe(last)
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	send := func(event alertEvent) bool {
		data, err := json.Marshal(event)
		if err != nil {
			return false
		}
		if _, err = fmt.Fprintf(w, "id: %d\n", event.ID); err != nil {
			return false
		}
		if _, err = fmt.Fprintf(w, "event: %s\n", event.Type); err != nil {
			return false
		}
		if _, err = fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}
	for _, event := range backlog {
		if !send(event) {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok || !send(event) {
				return
			}
		}
	}
}

func parseAlertActionPath(path string) (string, error) {
	value, err := url.PathUnescape(path)
	if err != nil {
		return "", err
	}
	return strings.TrimPrefix(value, "/"), nil
}
