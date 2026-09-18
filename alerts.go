package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
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

type alertRules struct {
	AgentNeedsYou     bool    `json:"agent_needs_you"`
	AgentStuck        bool    `json:"agent_stuck"`
	AgentDone         bool    `json:"agent_done"`
	AgentLimit        bool    `json:"agent_limit"`
	QuotaHigh         bool    `json:"quota_high"`
	BoxUnreachable    bool    `json:"box_unreachable"`
	BoxPressure       bool    `json:"box_pressure"`
	ProcessDied       bool    `json:"process_died"`
	ProbeFailed       bool    `json:"probe_failed"`
	StuckMinutes      int     `json:"stuckMinutes"`
	UnreachableChecks int     `json:"unreachableChecks"`
	PressureMinutes   int     `json:"pressureMinutes"`
	QuotaPercent      float64 `json:"quotaPercent"`
	LoadMultiplier    float64 `json:"loadMultiplier"`
	MemoryPercent     float64 `json:"memoryPercent"`
	SwapPercent       float64 `json:"swapPercent"`
	DiskPercent       float64 `json:"diskPercent"`
}

func defaultAlertRules() alertRules {
	return alertRules{
		AgentNeedsYou: true, AgentStuck: true, AgentLimit: true, QuotaHigh: true,
		BoxUnreachable: true, BoxPressure: true, ProcessDied: true, ProbeFailed: true,
		StuckMinutes: 10, UnreachableChecks: 3, PressureMinutes: 5, QuotaPercent: 90,
		LoadMultiplier: 2, MemoryPercent: 10, SwapPercent: 10, DiskPercent: 92,
	}
}

func normalizeAlertRules(value alertRules) alertRules {
	defaults := defaultAlertRules()
	if value.AgentNeedsYou {
		defaults.AgentNeedsYou = true
	}
	if value.AgentStuck {
		defaults.AgentStuck = true
	}
	if value.AgentDone {
		defaults.AgentDone = true
	}
	if value.AgentLimit {
		defaults.AgentLimit = true
	}
	if value.QuotaHigh {
		defaults.QuotaHigh = true
	}
	if value.BoxUnreachable {
		defaults.BoxUnreachable = true
	}
	if value.BoxPressure {
		defaults.BoxPressure = true
	}
	if value.ProcessDied {
		defaults.ProcessDied = true
	}
	if value.ProbeFailed {
		defaults.ProbeFailed = true
	}
	if value.StuckMinutes > 0 {
		defaults.StuckMinutes = value.StuckMinutes
	}
	if value.UnreachableChecks > 0 {
		defaults.UnreachableChecks = value.UnreachableChecks
	}
	if value.PressureMinutes > 0 {
		defaults.PressureMinutes = value.PressureMinutes
	}
	if value.QuotaPercent > 0 {
		defaults.QuotaPercent = value.QuotaPercent
	}
	if value.LoadMultiplier > 0 {
		defaults.LoadMultiplier = value.LoadMultiplier
	}
	if value.MemoryPercent > 0 {
		defaults.MemoryPercent = value.MemoryPercent
	}
	if value.SwapPercent > 0 {
		defaults.SwapPercent = value.SwapPercent
	}
	if value.DiskPercent > 0 {
		defaults.DiskPercent = value.DiskPercent
	}
	return defaults
}

type alertQuiet struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type alertProbe struct {
	Name    string        `json:"name"`
	Cmd     string        `json:"cmd"`
	Every   time.Duration `json:"-"`
	Timeout time.Duration `json:"-"`
}

func parseAlertDuration(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(strings.TrimSpace(value))
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func (p *alertProbe) UnmarshalJSON(data []byte) error {
	var value struct {
		Name    string `json:"name"`
		Cmd     string `json:"cmd"`
		Every   string `json:"every"`
		Timeout string `json:"timeout"`
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return err
	}
	p.Name, p.Cmd = strings.TrimSpace(value.Name), value.Cmd
	p.Every, p.Timeout = parseAlertDuration(value.Every, 5*time.Minute), parseAlertDuration(value.Timeout, 30*time.Second)
	return nil
}

func (p alertProbe) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Name    string `json:"name"`
		Cmd     string `json:"cmd"`
		Every   string `json:"every"`
		Timeout string `json:"timeout"`
	}{p.Name, p.Cmd, p.Every.String(), p.Timeout.String()})
}

type alertSink struct {
	Name        string   `json:"name,omitempty"`
	Type        string   `json:"type"`
	URL         string   `json:"url,omitempty"`
	Topic       string   `json:"topic,omitempty"`
	Token       string   `json:"token,omitempty"`
	Secret      string   `json:"secret,omitempty"`
	ChatID      string   `json:"chatId,omitempty"`
	MinSeverity string   `json:"minSeverity,omitempty"`
	Rules       []string `json:"rules,omitempty"`
	DeckURL     string   `json:"deckURL,omitempty"`
	AuthToken   string   `json:"-"`
}

type alertConfig struct {
	Rules              alertRules   `json:"rules"`
	RulesComplete      bool         `json:"rulesComplete,omitempty"`
	Probes             []alertProbe `json:"probes,omitempty"`
	Quiet              alertQuiet   `json:"quiet,omitempty"`
	QuietAllowCritical bool         `json:"quietAllowCritical,omitempty"`
	Sinks              []alertSink  `json:"sinks,omitempty"`
	WatchPIDs          []int        `json:"watchPids,omitempty"`
	WatchApps          []string     `json:"watchApps,omitempty"`
}

type AlertAction struct {
	Label  string `json:"label"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Body   any    `json:"body,omitempty"`
}

type Alert struct {
	ID           string        `json:"id"`
	Box          string        `json:"box"`
	Rule         string        `json:"rule"`
	Severity     string        `json:"severity"`
	Title        string        `json:"title"`
	Body         string        `json:"body"`
	At           time.Time     `json:"at"`
	State        string        `json:"state"`
	Link         string        `json:"link"`
	Actions      []AlertAction `json:"actions"`
	Count        int           `json:"count"`
	SnoozedUntil *time.Time    `json:"snoozedUntil,omitempty"`
	Subject      string        `json:"-"`
	LastAt       time.Time     `json:"-"`
}

type alertEvent struct {
	ID    uint64    `json:"id"`
	Type  string    `json:"type"`
	At    time.Time `json:"at"`
	Alert *Alert    `json:"alert,omitempty"`
	Data  object    `json:"data,omitempty"`
}

type alertStore struct {
	Alerts   []*Alert     `json:"alerts"`
	Events   []alertEvent `json:"events"`
	NextID   uint64       `json:"nextEventId"`
	Disarmed bool         `json:"disarmed"`
}

type alertDelivery struct {
	At      time.Time `json:"at"`
	Status  string    `json:"status"`
	Message string    `json:"message,omitempty"`
	Tries   int       `json:"tries"`
}

type alertProbeState struct {
	Failed  bool
	Running bool
	LastRun time.Time
}
type alertActivity struct {
	Signature string
	Changed   time.Time
	Stuck     bool
	Status    string
}

type alertSnapshot struct {
	Box       string
	Agents    []agentInfo
	Tails     map[string]string
	Boxes     []boxCard
	Usage     usageResponse
	Processes []process
	Apps      []appView
}

type alertManager struct {
	mintAction    func(alertID string) string
	sendAPNS      func(context.Context, pushDevice, Alert) error
	push          *pushRegistry
	mu            sync.Mutex
	cfg           alertConfig
	path          string
	alerts        map[string]*Alert
	events        []alertEvent
	nextID        uint64
	disarmed      bool
	subs          map[chan alertEvent]struct{}
	delivery      map[string]alertDelivery
	activity      map[string]alertActivity
	boxFailures   map[string]int
	boxPressure   map[string]time.Time
	boxReachable  map[string]bool
	agentStates   map[string]string
	probeStates   map[string]*alertProbeState
	probeMu       sync.Mutex
	processSeen   map[int]bool
	processRaised map[int]bool
	appRaised     map[string]bool
	now           func() time.Time
}

func alertConfigFromConfig(cfg config) alertConfig {
	value := cfg.Alerts
	if value.Rules == (alertRules{}) && cfg.AlertRules != (alertRules{}) {
		value.Rules = cfg.AlertRules
	}
	if cfg.AlertRulesComplete || value.RulesComplete {
		value.Rules, value.RulesComplete = cfg.AlertRules, true
	} else {
		value.Rules = normalizeAlertRules(value.Rules)
	}
	if cfg.Probes != nil {
		value.Probes = cfg.Probes
	}
	if cfg.Quiet.From != "" || cfg.Quiet.To != "" {
		value.Quiet = cfg.Quiet
	}
	if cfg.QuietAllowCritical {
		value.QuietAllowCritical = true
	}
	if cfg.Sinks != nil {
		value.Sinks = cfg.Sinks
	}
	if cfg.WatchPIDs != nil {
		value.WatchPIDs = cfg.WatchPIDs
	}
	if cfg.WatchApps != nil {
		value.WatchApps = cfg.WatchApps
	}
	if cfg.Apns.enabled() {
		found := false
		for _, sink := range value.Sinks {
			if strings.EqualFold(sink.Type, "apns") {
				found = true
				break
			}
		}
		if !found {
			value.Sinks = append(value.Sinks, alertSink{Name: "apns", Type: "apns"})
		}
	}
	return value
}

func newAlertManager(cfg config) *alertManager {
	return newAlertManagerAt(alertConfigFromConfig(cfg), filepath.Join(cfg.home, ".local", "share", "boxdeck", "alerts.json"))
}

func newAlertManagerAt(cfg alertConfig, path string) *alertManager {
	if !cfg.RulesComplete {
		cfg.Rules = normalizeAlertRules(cfg.Rules)
	}
	m := &alertManager{
		cfg: cfg, path: path, alerts: map[string]*Alert{}, subs: map[chan alertEvent]struct{}{},
		delivery: map[string]alertDelivery{}, activity: map[string]alertActivity{},
		boxFailures: map[string]int{}, boxPressure: map[string]time.Time{}, boxReachable: map[string]bool{},
		agentStates: map[string]string{}, probeStates: map[string]*alertProbeState{},
		processSeen: map[int]bool{}, processRaised: map[int]bool{}, appRaised: map[string]bool{}, now: time.Now,
	}
	m.load()
	return m
}

func (m *alertManager) load() {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var stored alertStore
	if json.Unmarshal(b, &stored) != nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, alert := range stored.Alerts {
		if alert == nil || alert.ID == "" {
			continue
		}
		if alert.Subject == "" {
			alert.Subject = alert.Title
		}
		if alert.LastAt.IsZero() {
			alert.LastAt = alert.At
		}
		m.alerts[alert.ID] = alert
	}
	m.events, m.nextID, m.disarmed = stored.Events, stored.NextID, stored.Disarmed
	for _, event := range m.events {
		if event.ID >= m.nextID {
			m.nextID = event.ID + 1
		}
	}
}

func (m *alertManager) persistLocked() error {
	if m.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	alerts := make([]*Alert, 0, len(m.alerts))
	for _, alert := range m.alerts {
		copy := *alert
		copy.Subject, copy.LastAt = "", time.Time{}
		alerts = append(alerts, &copy)
	}
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].At.Before(alerts[j].At) })
	data, err := json.MarshalIndent(alertStore{Alerts: alerts, Events: m.events, NextID: m.nextID, Disarmed: m.disarmed}, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}

func (m *alertManager) pruneLocked(now time.Time) {
	cutoff := now.Add(-30 * 24 * time.Hour)
	for id, alert := range m.alerts {
		if alert.At.Before(cutoff) {
			delete(m.alerts, id)
		}
	}
	if len(m.alerts) > 5000 {
		all := make([]*Alert, 0, len(m.alerts))
		for _, alert := range m.alerts {
			all = append(all, alert)
		}
		sort.Slice(all, func(i, j int) bool { return all[i].At.Before(all[j].At) })
		for _, alert := range all[:len(all)-5000] {
			delete(m.alerts, alert.ID)
		}
	}
	if len(m.events) > 5000 {
		m.events = append([]alertEvent(nil), m.events[len(m.events)-5000:]...)
	}
}

func (m *alertManager) recordEventLocked(eventType string, alert *Alert, data object) alertEvent {
	event := alertEvent{ID: m.nextID, Type: eventType, At: m.now(), Alert: alert, Data: data}
	if m.nextID == 0 {
		event.ID = 1
	}
	m.nextID = event.ID + 1
	m.events = append(m.events, event)
	for subscriber := range m.subs {
		select {
		case subscriber <- event:
		default:
		}
	}
	return event
}

func (m *alertManager) raise(input Alert) (Alert, bool) {
	if input.At.IsZero() {
		input.At = m.now()
	}
	if input.State == "" {
		input.State = "open"
	}
	if input.Count < 1 {
		input.Count = 1
	}
	if input.Subject == "" {
		input.Subject = input.Title
	}
	if input.Severity != "info" && input.Severity != "warning" && input.Severity != "critical" {
		input.Severity = "warning"
	}
	if input.Actions == nil {
		input.Actions = []AlertAction{}
	}
	if input.Link == "" {
		input.Link = alertLink(input.Rule, input.Subject)
	}
	input.LastAt = input.At
	m.mu.Lock()
	now := m.now()
	m.pruneLocked(now)
	var existing *Alert
	for _, alert := range m.alerts {
		if alert.Rule == input.Rule && alert.Box == input.Box && alert.Subject == input.Subject && now.Sub(alert.LastAt) < 15*time.Minute && alert.State != "resolved" {
			existing = alert
			break
		}
	}
	if existing != nil {
		existing.Count++
		existing.At, existing.LastAt, existing.Title, existing.Body = input.At, input.At, input.Title, input.Body
		if len(input.Actions) > 0 {
			existing.Actions = input.Actions
		}
		if input.Link != "" {
			existing.Link = input.Link
		}
		copy := *existing
		m.recordEventLocked("alert", &copy, nil)
		_ = m.persistLocked()
		m.mu.Unlock()
		return copy, m.shouldDeliver(copy)
	}
	if input.ID == "" {
		input.ID = fmt.Sprintf("a-%d-%d", input.At.UnixNano(), m.nextID)
	}
	hasSnooze := false
	for _, action := range input.Actions {
		if action.Label == "Snooze 2h" {
			hasSnooze = true
			break
		}
	}
	if !hasSnooze {
		input.Actions = append(input.Actions, AlertAction{Label: "Snooze 2h", Method: http.MethodPost, Path: "/api/alerts/" + input.ID + "/snooze", Body: object{"until": "2h"}})
	}
	copy := input
	m.alerts[input.ID] = &copy
	m.recordEventLocked("alert", &copy, nil)
	_ = m.persistLocked()
	deliver := m.shouldDeliverLocked(copy)
	sinks := append([]alertSink(nil), m.cfg.Sinks...)
	m.mu.Unlock()
	if deliver && len(sinks) > 0 {
		go m.deliver(context.Background(), sinks, copy)
	}
	return copy, deliver
}

func (m *alertManager) shouldDeliver(alert Alert) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.shouldDeliverLocked(alert)
}

func (m *alertManager) shouldDeliverLocked(alert Alert) bool {
	if m.disarmed || (quietAt(m.cfg.Quiet, m.now()) && !(alert.Severity == "critical" && m.cfg.QuietAllowCritical)) {
		return false
	}
	if alert.State == "snoozed" && alert.SnoozedUntil != nil && alert.SnoozedUntil.After(m.now()) {
		return false
	}
	return true
}

func quietAt(quiet alertQuiet, now time.Time) bool {
	if quiet.From == "" || quiet.To == "" {
		return false
	}
	from, err1 := time.Parse("15:04", quiet.From)
	to, err2 := time.Parse("15:04", quiet.To)
	if err1 != nil || err2 != nil {
		return false
	}
	minutes := now.Hour()*60 + now.Minute()
	start, end := from.Hour()*60+from.Minute(), to.Hour()*60+to.Minute()
	if start == end {
		return true
	}
	if start < end {
		return minutes >= start && minutes < end
	}
	return minutes >= start || minutes < end
}

func (m *alertManager) list(state, since, box, rule string) []Alert {
	var sinceAt time.Time
	if since != "" {
		sinceAt, _ = time.Parse(time.RFC3339, since)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []Alert{}
	changed := false
	for _, alert := range m.alerts {
		if alert.State == "snoozed" && alert.SnoozedUntil != nil && !alert.SnoozedUntil.After(m.now()) {
			alert.State, alert.SnoozedUntil, changed = "open", nil, true
		}
		if state != "" && alert.State != state || !sinceAt.IsZero() && alert.At.Before(sinceAt) || box != "" && alert.Box != box || rule != "" && alert.Rule != rule {
			continue
		}
		copy := *alert
		result = append(result, copy)
	}
	if changed {
		_ = m.persistLocked()
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].At.Equal(result[j].At) {
			return result[i].ID > result[j].ID
		}
		return result[i].At.After(result[j].At)
	})
	return result
}

func (m *alertManager) get(id string) (Alert, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	alert, ok := m.alerts[id]
	if !ok {
		return Alert{}, false
	}
	copy := *alert
	return copy, true
}

func (m *alertManager) change(id, state string, until *time.Time) (Alert, error) {
	m.mu.Lock()
	alert, ok := m.alerts[id]
	if !ok {
		m.mu.Unlock()
		return Alert{}, os.ErrNotExist
	}
	alert.State, alert.SnoozedUntil = state, until
	copy := *alert
	m.recordEventLocked("state", &copy, object{"state": state})
	_ = m.persistLocked()
	m.mu.Unlock()
	return copy, nil
}

func (m *alertManager) ack(id string) (Alert, error)     { return m.change(id, "acked", nil) }
func (m *alertManager) resolve(id string) (Alert, error) { return m.change(id, "resolved", nil) }
func (m *alertManager) snooze(id string, until time.Time) error {
	_, err := m.change(id, "snoozed", &until)
	return err
}

func (m *alertManager) snoozeAll(until time.Time) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, alert := range m.alerts {
		if alert.State != "open" && alert.State != "acked" {
			continue
		}
		alert.State, alert.SnoozedUntil = "snoozed", &until
		copy := *alert
		m.recordEventLocked("state", &copy, object{"state": "snoozed"})
		n++
	}
	_ = m.persistLocked()
	return n
}

func (m *alertManager) setDisarmed(on bool) {
	m.mu.Lock()
	m.disarmed = on
	m.recordEventLocked("disarm", nil, object{"on": on})
	_ = m.persistLocked()
	m.mu.Unlock()
}

func (m *alertManager) eventsAfter(id uint64) []alertEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := []alertEvent{}
	for _, event := range m.events {
		if event.ID > id {
			result = append(result, event)
		}
	}
	return result
}

func (m *alertManager) subscribe(after uint64) (<-chan alertEvent, []alertEvent, func()) {
	m.mu.Lock()
	backlog := []alertEvent{}
	for _, event := range m.events {
		if event.ID > after {
			backlog = append(backlog, event)
		}
	}
	ch := make(chan alertEvent, 32)
	m.subs[ch] = struct{}{}
	m.mu.Unlock()
	return ch, backlog, func() { m.mu.Lock(); delete(m.subs, ch); close(ch); m.mu.Unlock() }
}

func alertLink(rule, subject string) string {
	subject = urlQuerySubject(subject)
	switch {
	case strings.HasPrefix(rule, "agent_"):
		return "#/agents?pane=" + subject
	case strings.HasPrefix(rule, "quota_"):
		return "#/usage"
	case strings.HasPrefix(rule, "box_"):
		return "#/boxes"
	default:
		return "#/overview"
	}
}

func urlQuerySubject(value string) string {
	return strings.NewReplacer(" ", "%20", "#", "%23", "?", "%3F", "&", "%26").Replace(value)
}

func normalizeAgentStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(strings.ReplaceAll(status, "-", "_")))
	if status == "" {
		return "working"
	}
	return status
}

var alertLimitRE = regexp.MustCompile("(?i)\\b(?:usage|rate|session|context)\\s*(?:limit|exhausted|reset)|\\b(?:rate|usage|session)\\s*limited")

func (m *alertManager) evaluate(snapshot alertSnapshot) {
	m.mu.Lock()
	cfg, now := m.cfg, m.now()
	m.mu.Unlock()
	rules := cfg.Rules
	if rules == (alertRules{}) {
		rules = defaultAlertRules()
	}
	seen := map[string]bool{}
	for _, agent := range snapshot.Agents {
		subject := agent.Pane
		if subject == "" {
			subject = agent.PaneID
		}
		if subject == "" {
			subject = strconv.Itoa(agent.PID)
		}
		seen[subject] = true
		status := normalizeAgentStatus(agent.Status)
		tail := snapshot.Tails[agent.Pane]
		if tail == "" {
			tail = snapshot.Tails[agent.PaneID]
		}
		needs := status == "waiting" || status == "blocked" || status == "needs_you" || paneNeedsInput(tail)
		if needs && rules.AgentNeedsYou {
			key := "needs:" + subject
			if m.conditionFirst(key) {
				m.raise(Alert{Box: snapshot.Box, Rule: "agent_needs_you", Subject: subject, Severity: "warning", Title: "Agent needs you", Body: subject + " is waiting for input", Link: "#/agents?pane=" + urlQuerySubject(subject), Actions: agentActions(subject)})
			}
		} else {
			m.conditionClear("needs:" + subject)
		}
		if rules.AgentLimit && alertLimitRE.MatchString(tail+" "+agent.Status) {
			key := "limit:" + subject
			if m.conditionFirst(key) {
				m.raise(Alert{Box: snapshot.Box, Rule: "agent_limit", Subject: subject, Severity: "warning", Title: "Agent limit reached", Body: subject + " reported a usage or session limit", Link: "#/agents?pane=" + urlQuerySubject(subject), Actions: agentActions(subject)})
			}
		} else {
			m.conditionClear("limit:" + subject)
		}

		working := status == "working" || status == "running"
		m.mu.Lock()
		activity := m.activity[subject]
		signature := tail
		if signature != activity.Signature {
			activity.Signature, activity.Changed, activity.Stuck = signature, now, false
		}
		if activity.Changed.IsZero() {
			activity.Changed = now
		}
		previous := m.agentStates[subject]
		activity.Status, m.agentStates[subject] = status, status
		m.activity[subject] = activity
		m.mu.Unlock()
		hasTerminal := tail != "" || agent.Kind == "tmux"
		if working && rules.AgentStuck && hasTerminal && now.Sub(activity.Changed) >= time.Duration(maxInt(rules.StuckMinutes, 10))*time.Minute && !activity.Stuck {
			m.mu.Lock()
			current := m.activity[subject]
			current.Stuck = true
			m.activity[subject] = current
			m.mu.Unlock()
			m.raise(Alert{Box: snapshot.Box, Rule: "agent_stuck", Subject: subject, Severity: "warning", Title: "Agent appears stuck", Body: subject + " has produced no terminal output for " + strconv.Itoa(maxInt(rules.StuckMinutes, 10)) + " minutes", Link: "#/agents?pane=" + urlQuerySubject(subject), Actions: agentActions(subject)})
		}
		if rules.AgentDone && (previous == "working" || previous == "running") && (status == "idle" || status == "done") {
			m.raise(Alert{Box: snapshot.Box, Rule: "agent_done", Subject: subject, Severity: "info", Title: "Agent finished", Body: subject + " is now " + status, Link: "#/agents?pane=" + urlQuerySubject(subject)})
		}
	}
	m.mu.Lock()
	for subject := range m.activity {
		if !seen[subject] {
			delete(m.activity, subject)
			delete(m.agentStates, subject)
		}
	}
	m.mu.Unlock()
	m.evaluateBoxes(snapshot, rules, now)
	m.evaluateQuota(snapshot, rules)
	m.evaluateProcesses(snapshot, rules)
}

func agentActions(subject string) []AlertAction {
	return []AlertAction{
		{Label: "Approve", Method: http.MethodPost, Path: "/api/herd/keys", Body: object{"pane": subject, "keys": "Enter"}},
		{Label: "Yes", Method: http.MethodPost, Path: "/api/herd/keys", Body: object{"pane": subject, "keys": "y"}},
		{Label: "No", Method: http.MethodPost, Path: "/api/herd/keys", Body: object{"pane": subject, "keys": "n"}},
		{Label: "Interrupt", Method: http.MethodPost, Path: "/api/herd/interrupt", Body: object{"pane": subject}},
	}
}

func (m *alertManager) conditionFirst(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.activity[key].Stuck {
		return false
	}
	m.activity[key] = alertActivity{Stuck: true}
	return true
}

func (m *alertManager) conditionClear(key string) {
	m.mu.Lock()
	delete(m.activity, key)
	m.mu.Unlock()
}

func (m *alertManager) evaluateBoxes(snapshot alertSnapshot, rules alertRules, now time.Time) {
	for _, box := range snapshot.Boxes {
		if box.Local {
			continue
		}
		key := box.URL
		m.mu.Lock()
		previous, hadPrevious := m.boxReachable[key]
		m.boxReachable[key] = box.OK
		if !box.OK {
			m.boxFailures[key]++
		} else {
			m.boxFailures[key] = 0
		}
		failures := m.boxFailures[key]
		m.mu.Unlock()
		if hadPrevious && previous != box.OK {
			m.recordEvent("box_reachability", nil, object{"box": box.Name, "ok": box.OK})
		}
		threshold := maxInt(rules.UnreachableChecks, 3)
		if !box.OK && rules.BoxUnreachable && failures == threshold {
			m.raise(Alert{Box: box.Name, Rule: "box_unreachable", Subject: key, Severity: "critical", Title: "Box unreachable", Body: box.Name + " failed " + strconv.Itoa(failures) + " health checks", Link: "#/boxes"})
		}
		if box.OK && rules.BoxPressure {
			reason := pressureReason(box.Health, rules)
			m.mu.Lock()
			started := m.boxPressure[key]
			if reason == "" {
				delete(m.boxPressure, key)
			} else if started.IsZero() {
				m.boxPressure[key] = now
				started = now
			}
			m.mu.Unlock()
			if reason != "" && !started.IsZero() && now.Sub(started) >= time.Duration(maxInt(rules.PressureMinutes, 5))*time.Minute {
				m.raise(Alert{Box: box.Name, Rule: "box_pressure", Subject: key + ":" + reason, Severity: "warning", Title: "Box under pressure", Body: box.Name + ": " + reason, Link: "#/boxes"})
			}
		}
	}
}

func maxInt(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}
func maxFloat(value, fallback float64) float64 {
	if value > 0 {
		return value
	}
	return fallback
}
func numberValue(value any) float64 {
	switch number := value.(type) {
	case float64:
		return number
	case float32:
		return float64(number)
	case int:
		return float64(number)
	case int64:
		return float64(number)
	default:
		return 0
	}
}

func pressureReason(health object, rules alertRules) string {
	load, cores := numberValue(health["load1"]), numberValue(health["cores"])
	if cores <= 0 {
		cores = 1
	}
	multiplier := maxFloat(rules.LoadMultiplier, 2)
	if load > cores*multiplier {
		return "load is above cores x " + strconv.FormatFloat(multiplier, 'f', -1, 64)
	}
	total, available := numberValue(health["memTotal"]), numberValue(health["memAvail"])
	if total > 0 && available/total*100 < maxFloat(rules.MemoryPercent, 10) {
		return "memory available is under " + strconv.FormatFloat(maxFloat(rules.MemoryPercent, 10), 'f', -1, 64) + "%"
	}
	swapTotal, swapFree := numberValue(health["swapTotal"]), numberValue(health["swapFree"])
	if swapTotal > 0 && swapFree/swapTotal*100 < maxFloat(rules.SwapPercent, 10) {
		return "swap free is under " + strconv.FormatFloat(maxFloat(rules.SwapPercent, 10), 'f', -1, 64) + "%"
	}
	if numberValue(health["diskPct"]) > maxFloat(rules.DiskPercent, 92) {
		return "disk is over " + strconv.FormatFloat(maxFloat(rules.DiskPercent, 92), 'f', -1, 64) + "%"
	}
	return ""
}

func (m *alertManager) evaluateQuota(snapshot alertSnapshot, rules alertRules) {
	if !rules.QuotaHigh {
		return
	}
	for provider, usage := range snapshot.Usage.Providers {
		if usage.Quota == nil {
			continue
		}
		for label, window := range map[string]*quotaWindow{"5h": usage.Quota.FiveHour, "7d": usage.Quota.SevenDay} {
			if window != nil && window.Pct > maxFloat(rules.QuotaPercent, 90) {
				subject := provider + ":" + label
				m.raise(Alert{Box: snapshot.Box, Rule: "quota_high", Subject: subject, Severity: "warning", Title: "Quota is high", Body: provider + " " + label + " is at " + strconv.FormatFloat(window.Pct, 'f', 0, 64) + "%", Link: "#/usage"})
			}
		}
	}
}

func alertContainsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (m *alertManager) evaluateProcesses(snapshot alertSnapshot, rules alertRules) {
	if !rules.ProcessDied {
		return
	}
	current := map[int]bool{}
	for _, process := range snapshot.Processes {
		current[process.PID] = true
	}
	m.mu.Lock()
	watched := append([]int(nil), m.cfg.WatchPIDs...)
	for _, pid := range watched {
		if current[pid] {
			m.processSeen[pid] = true
			delete(m.processRaised, pid)
		} else if m.processSeen[pid] && !m.processRaised[pid] {
			m.processRaised[pid] = true
			go m.raise(Alert{Box: snapshot.Box, Rule: "process_died", Subject: strconv.Itoa(pid), Severity: "critical", Title: "Watched process exited", Body: "PID " + strconv.Itoa(pid) + " is no longer running", Link: "#/processes"})
		}
	}
	apps := append([]string(nil), m.cfg.WatchApps...)
	m.mu.Unlock()
	for _, view := range snapshot.Apps {
		if view.ExitCode == nil || *view.ExitCode == 0 || !alertContainsString(apps, view.ID) {
			continue
		}
		m.mu.Lock()
		raised := m.appRaised[view.ID]
		if !raised {
			m.appRaised[view.ID] = true
		}
		m.mu.Unlock()
		if !raised {
			m.raise(Alert{Box: snapshot.Box, Rule: "process_died", Subject: "app:" + view.ID, Severity: "critical", Title: "App exited", Body: view.Name + " exited with code " + strconv.Itoa(*view.ExitCode), Link: "#/apps", Actions: []AlertAction{{Label: "Restart", Method: http.MethodPost, Path: "/api/apps/" + view.ID + "/restart"}}})
		}
	}
}

func (m *alertManager) recordEvent(eventType string, alert *Alert, data object) {
	m.mu.Lock()
	m.recordEventLocked(eventType, alert, data)
	_ = m.persistLocked()
	m.mu.Unlock()
}

func (m *alertManager) start(ctx context.Context, snapshot func() alertSnapshot) {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				m.evaluate(snapshot())
			}
		}
	}()
	go m.runProbes(ctx)
}

func (m *alertManager) runProbes(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.mu.Lock()
			probes := append([]alertProbe(nil), m.cfg.Probes...)
			m.mu.Unlock()
			for _, probe := range probes {
				if strings.TrimSpace(probe.Name) == "" || strings.TrimSpace(probe.Cmd) == "" {
					continue
				}
				m.probeMu.Lock()
				state := m.probeStates[probe.Name]
				if state == nil {
					state = &alertProbeState{}
					m.probeStates[probe.Name] = state
				}
				due := state.LastRun.IsZero() || time.Since(state.LastRun) >= maxDuration(probe.Every, time.Minute)
				if state.Running || !due {
					m.probeMu.Unlock()
					continue
				}
				state.Running, state.LastRun = true, m.now()
				m.probeMu.Unlock()
				go func(probe alertProbe) {
					err := m.runProbe(ctx, probe)
					m.recordProbeResult(probe, err != nil, errorText(err))
					m.probeMu.Lock()
					m.probeStates[probe.Name].Running = false
					m.probeMu.Unlock()
				}(probe)
			}
		}
	}
}

func maxDuration(value, fallback time.Duration) time.Duration {
	if value > 0 {
		return value
	}
	return fallback
}
func errorText(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (m *alertManager) runProbe(ctx context.Context, probe alertProbe) error {
	commandCtx, cancel := context.WithTimeout(ctx, maxDuration(probe.Timeout, 30*time.Second))
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "bash", "-lc", probe.Cmd)
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if commandCtx.Err() != nil {
		return commandCtx.Err()
	}
	return err
}

func (m *alertManager) recordProbeResult(probe alertProbe, failed bool, detail string) {
	m.probeMu.Lock()
	state := m.probeStates[probe.Name]
	if state == nil {
		state = &alertProbeState{}
		m.probeStates[probe.Name] = state
	}
	previous := state.Failed
	state.Failed = failed
	m.probeMu.Unlock()
	if failed && !previous {
		m.raise(Alert{Rule: "probe_failed", Subject: probe.Name, Severity: "critical", Title: "Probe failed", Body: probe.Name + ": " + detail, Link: "#/overview"})
	}
	if !failed && previous {
		m.raise(Alert{Rule: "probe_failed", Subject: probe.Name + ":recovery", Severity: "info", Title: "Probe recovered", Body: probe.Name + " is healthy again", Link: "#/overview"})
	}
}

func maxDurationForProbe(p alertProbe) time.Duration { return maxDuration(p.Every, time.Minute) }

func (m *alertManager) deliver(ctx context.Context, sinks []alertSink, alert Alert) {
	for _, sink := range sinks {
		if sinkAllows(sink, alert) {
			_ = m.deliverOne(ctx, sink, alert)
		}
	}
}

func sinkAllows(sink alertSink, alert Alert) bool {
	ranks := map[string]int{"info": 0, "warning": 1, "critical": 2}
	if sink.MinSeverity != "" && ranks[alert.Severity] < ranks[sink.MinSeverity] {
		return false
	}
	if len(sink.Rules) > 0 && !alertContainsString(sink.Rules, alert.Rule) {
		return false
	}
	return true
}

func (m *alertManager) deliverOne(ctx context.Context, sink alertSink, alert Alert) error {
	if sink.Name == "" {
		sink.Name = sink.Type
	}
	var err error
	tries := 0
	for tries = 1; tries <= 3; tries++ {
		err = m.deliverAttempt(ctx, sink, alert)
		if err == nil {
			break
		}
		if tries < 3 {
			select {
			case <-ctx.Done():
				break
			case <-time.After(time.Duration(tries) * 100 * time.Millisecond):
			}
		}
	}
	m.mu.Lock()
	status := "delivered"
	if err != nil {
		status = "failed"
	}
	m.delivery[sink.Name] = alertDelivery{At: m.now(), Status: status, Message: errorText(err), Tries: tries}
	_ = m.persistLocked()
	m.mu.Unlock()
	return err
}

func (m *alertManager) deliverAttempt(ctx context.Context, sink alertSink, alert Alert) error {
	switch strings.ToLower(sink.Type) {
	case "ntfy":
		if sink.URL == "" || sink.Topic == "" {
			return errors.New("ntfy url and topic are required")
		}
		target := strings.TrimRight(sink.URL, "/") + "/" + strings.TrimLeft(sink.Topic, "/")
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(alert.Body))
		if err != nil {
			return err
		}
		request.Header.Set("Title", alert.Title)
		request.Header.Set("Priority", ntfyPriority(alert.Severity))
		request.Header.Set("Tags", ntfyTag(alert.Severity))
		request.Header.Set("Click", strings.TrimRight(sink.DeckURL, "/")+alert.Link)
		request.Header.Set("Actions", ntfyActions(sink.DeckURL, m.actionTokenFor(alert), alert.Actions))
		if sink.Token != "" {
			request.Header.Set("Authorization", "Bearer "+sink.Token)
		}
		return doDelivery(request)
	case "webhook":
		if sink.URL == "" {
			return errors.New("webhook url is required")
		}
		body, _ := json.Marshal(alert)
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, sink.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/json")
		sum := hmac.New(sha256.New, []byte(sink.Secret))
		_, _ = sum.Write(body)
		request.Header.Set("X-Boxdeck-Signature", "sha256="+hex.EncodeToString(sum.Sum(nil)))
		return doDelivery(request)
	case "slack":
		body, _ := json.Marshal(object{"text": alert.Title + "\n" + alert.Body})
		return postJSON(ctx, sink.URL, body)
	case "telegram":
		body, _ := json.Marshal(object{"chat_id": sink.ChatID, "text": alert.Title + "\n" + alert.Body})
		return postJSON(ctx, strings.TrimRight(sink.URL, "/")+"/bot"+sink.Token+"/sendMessage", body)
	case "pushover":
		body := []byte("token=" + urlQuerySubject(sink.Token) + "&user=" + urlQuerySubject(sink.ChatID) + "&title=" + urlQuerySubject(alert.Title) + "&message=" + urlQuerySubject(alert.Body))
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, sink.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return doDelivery(request)
	case "apns":
		if m.sendAPNS == nil || m.push == nil {
			return errors.New("apns delivery is not configured")
		}
		devices := m.push.list()
		if len(devices) == 0 {
			return errors.New("no registered push devices")
		}
		var failures []string
		for _, device := range devices {
			err := m.sendAPNS(ctx, device, alert)
			status := alertDelivery{At: m.now(), Status: "delivered", Tries: 1}
			if err != nil {
				status.Status, status.Message = "failed", errorText(err)
				failures = append(failures, device.Name+": "+err.Error())
				if isAPNSUnregistered(err) {
					_, _ = m.push.remove(pushDeviceID(device), "", "")
				}
			}
			_ = m.push.record(device, status)
		}
		if len(failures) > 0 {
			return errors.New(strings.Join(failures, "; "))
		}
		return nil
	case "desktop":
		command, args := "notify-send", []string{alert.Title, alert.Body}
		if _, err := exec.LookPath(command); err != nil {
			command, args = "osascript", []string{"-e", "display notification " + strconv.Quote(alert.Body) + " with title " + strconv.Quote(alert.Title)}
		}
		return exec.CommandContext(ctx, command, args...).Run()
	default:
		return fmt.Errorf("unknown alert sink %q", sink.Type)
	}
}

func ntfyPriority(severity string) string {
	if severity == "critical" {
		return "max"
	}
	if severity == "warning" {
		return "high"
	}
	return "default"
}
func ntfyTag(severity string) string {
	if severity == "critical" {
		return "rotating_light"
	}
	if severity == "warning" {
		return "warning"
	}
	return "information_source"
}
func ntfyActions(base, token string, actions []AlertAction) string {
	parts := []string{}
	for _, action := range actions {
		if action.Method != http.MethodPost || action.Path == "" {
			continue
		}
		body, _ := json.Marshal(action.Body)
		actionText := "http, " + action.Label + ", " + strings.TrimRight(base, "/") + action.Path + ", body='" + strings.ReplaceAll(string(body), "'", "") + "', clear=true"
		if token != "" {
			actionText += ", headers='Authorization: Bearer " + strings.ReplaceAll(token, "'", "") + "'"
		}
		parts = append(parts, actionText)
	}
	return strings.Join(parts, "; ")
}

func doDelivery(request *http.Request) error {
	response, err := (&http.Client{Timeout: 10 * time.Second}).Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("%s, %d", request.URL.Hostname(), response.StatusCode)
	}
	return nil
}

func postJSON(ctx context.Context, target string, body []byte) error {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	return doDelivery(request)
}

func (m *alertManager) deliveryView() map[string]alertDelivery {
	m.mu.Lock()
	defer m.mu.Unlock()
	copy := map[string]alertDelivery{}
	for name, value := range m.delivery {
		copy[name] = value
	}
	return copy
}

func (a *app) alertSnapshot() alertSnapshot {
	procs, panes, herdr := a.collect.getProcesses(), a.collect.getPanes(), a.collect.getHerdr()
	agents := mergeAgents(procs, panes, herdr, a.cfg, paneFromEnvironment)
	tails := map[string]string{}
	for _, agent := range agents {
		if agent.Pane == "" {
			continue
		}
		text, _, err := readPane(a.ctx, a.cfg.home, agent.Pane)
		if err == nil {
			tails[agent.Pane] = text
		}
	}
	for _, pane := range panes {
		subject := fmt.Sprintf("%s:%d.%d", pane.Session, pane.Win, pane.Pane)
		if tails[subject] == "" {
			text, _, err := readPane(a.ctx, a.cfg.home, subject)
			if err == nil {
				tails[subject] = text
			}
		}
		found := false
		for _, agent := range agents {
			if agent.Pane == subject {
				found = true
				break
			}
		}
		if !found && strings.HasPrefix(strings.TrimSpace(pane.Command), "sleep") {
			agents = append(agents, agentInfo{Kind: "tmux", Status: "working", Pane: subject})
		}
	}
	boxes := a.boxes(a.ctx)
	return alertSnapshot{Box: a.cfg.Host, Agents: agents, Tails: tails, Boxes: boxes, Usage: a.usage.snapshot(a.ctx, 7), Processes: procs, Apps: a.apps.snapshot()}
}

func (a *app) startAlerts() {
	if a.alerts != nil {
		a.alerts.start(a.ctx, a.alertSnapshot)
	}
}

// actionTokenFor returns a per alert token for notification buttons, or "" when the app has not
// wired a minter (tests), in which case no Authorization header is sent at all.
func (m *alertManager) actionTokenFor(alert Alert) string {
	if m.mintAction == nil {
		return ""
	}
	return m.mintAction(alert.ID)
}
