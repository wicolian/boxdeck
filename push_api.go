package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type pushDeviceView struct {
	ID           string        `json:"id"`
	Platform     string        `json:"platform"`
	Name         string        `json:"name,omitempty"`
	BundleID     string        `json:"bundleId,omitempty"`
	TokenSuffix  string        `json:"tokenSuffix,omitempty"`
	LastDelivery alertDelivery `json:"lastDelivery,omitempty"`
}

type pushConfigView struct {
	Enabled     bool   `json:"enabled"`
	KeyID       string `json:"keyId,omitempty"`
	TeamID      string `json:"teamId,omitempty"`
	BundleID    string `json:"bundleId,omitempty"`
	Environment string `json:"environment"`
	HasKey      bool   `json:"hasKey"`
	Host        string `json:"apnsHost,omitempty"`
}

func (a *app) alertDeckURL() string {
	a.pushMu.Lock()
	cfg := a.cfg
	a.pushMu.Unlock()
	return "http://" + cfg.Host + ":" + fmt.Sprint(int(cfg.Port))
}

func (a *app) pushConfigView() pushConfigView {
	a.pushMu.Lock()
	cfg := a.cfg.Apns
	a.pushMu.Unlock()
	_, err := os.Stat(cfg.KeyPath)
	return pushConfigView{Enabled: cfg.enabled(), KeyID: cfg.KeyID, TeamID: cfg.TeamID, BundleID: cfg.BundleID, Environment: cfg.Environment, HasKey: err == nil, Host: cfg.Host}
}

func pushDeviceViewOf(device pushDevice) pushDeviceView {
	suffix := ""
	if len(device.Token) > 4 {
		suffix = device.Token[len(device.Token)-4:]
	}
	return pushDeviceView{ID: pushDeviceID(device), Platform: device.Platform, Name: device.Name, BundleID: device.BundleID, TokenSuffix: suffix, LastDelivery: device.Delivery}
}

func (a *app) pushAPI(w http.ResponseWriter, r *http.Request) {
	if a.push == nil {
		jsonReply(w, http.StatusServiceUnavailable, object{"error": "push is not available"})
		return
	}
	switch r.URL.Path {
	case "/api/push":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		devices := a.push.list()
		views := make([]pushDeviceView, 0, len(devices))
		for _, device := range devices {
			views = append(views, pushDeviceViewOf(device))
		}
		jsonReply(w, http.StatusOK, object{"config": a.pushConfigView(), "devices": views})
	case "/api/push/register":
		a.pushRegisterAPI(w, r)
	case "/api/push/test":
		a.pushTestAPI(w, r)
	case "/api/push/config":
		a.pushConfigAPI(w, r)
	case "/api/push/key":
		a.pushKeyAPI(w, r)
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "push route not found"})
	}
}

func (a *app) pushRegisterAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Platform string `json:"platform"`
			Token    string `json:"token"`
			Name     string `json:"name"`
			BundleID string `json:"bundleId"`
		}
		if !decodeJSONBody(w, r, &body, 16<<10) {
			return
		}
		body.Platform, body.Token, body.Name, body.BundleID = strings.ToLower(strings.TrimSpace(body.Platform)), strings.TrimSpace(body.Token), strings.TrimSpace(body.Name), strings.TrimSpace(body.BundleID)
		if !validPushPlatform(body.Platform) || body.Token == "" {
			jsonReply(w, http.StatusBadRequest, object{"error": "platform must be ios or watchos and token is required"})
			return
		}
		if len(body.Token) > 512 || strings.ContainsAny(body.Token, "\r\n") {
			jsonReply(w, http.StatusBadRequest, object{"error": "push token is invalid"})
			return
		}
		if body.BundleID == "" {
			a.pushMu.Lock()
			body.BundleID = a.cfg.Apns.BundleID
			a.pushMu.Unlock()
		}
		device := pushDevice{Platform: body.Platform, Token: body.Token, Name: body.Name, BundleID: body.BundleID}
		if err := a.push.register(device); err != nil {
			jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
			return
		}
		jsonReply(w, http.StatusCreated, pushDeviceViewOf(device))
	case http.MethodDelete:
		var body struct {
			ID       string `json:"id"`
			Platform string `json:"platform"`
			Token    string `json:"token"`
		}
		if !decodeJSONBody(w, r, &body, 16<<10) {
			return
		}
		removed, err := a.push.remove(strings.TrimSpace(body.ID), strings.ToLower(strings.TrimSpace(body.Platform)), strings.TrimSpace(body.Token))
		if err != nil {
			jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
			return
		}
		if !removed {
			jsonReply(w, http.StatusNotFound, object{"error": "push device not found"})
			return
		}
		jsonReply(w, http.StatusOK, object{"removed": true})
	default:
		w.Header().Set("Allow", "POST, DELETE")
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST or DELETE to register a push device"})
	}
}

func (a *app) pushTestAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	devices := a.push.list()
	if len(devices) == 0 {
		jsonReply(w, http.StatusBadRequest, object{"error": "no registered push devices"})
		return
	}
	testAlert := Alert{ID: "push-test-" + fmt.Sprint(time.Now().UnixNano()), Box: a.cfg.Host, Rule: "push_test", Severity: "info", Title: "Boxdeck test alert", Body: "Native push delivery is connected.", Link: "#/alerts"}
	results := make([]object, 0, len(devices))
	for _, device := range devices {
		err := a.sendPushDevice(r.Context(), device, testAlert, "")
		result := object{"id": pushDeviceID(device), "status": "delivered"}
		if err != nil {
			result["status"], result["message"] = "failed", err.Error()
			if isAPNSUnregistered(err) {
				_, _ = a.push.remove(pushDeviceID(device), "", "")
			}
		}
		results = append(results, result)
	}
	jsonReply(w, http.StatusOK, object{"results": results})
}

func (a *app) sendPushDevice(ctx context.Context, device pushDevice, alert Alert, actionToken string) error {
	a.pushMu.Lock()
	cfg := a.cfg.Apns
	a.pushMu.Unlock()
	if a.apns == nil {
		return errors.New("apns sender is not configured")
	}
	err := a.apns.send(ctx, cfg, device, alert, a.alertDeckURL(), actionToken)
	status := alertDelivery{At: time.Now(), Status: "delivered", Tries: 1}
	if err != nil {
		status.Status, status.Message = "failed", err.Error()
	}
	_ = a.push.record(device, status)
	return err
}

func (a *app) pushConfigAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost && r.Method != http.MethodPut {
		requireMethod(w, r, http.MethodPost)
		return
	}
	var body struct {
		KeyID       string `json:"keyId"`
		TeamID      string `json:"teamId"`
		BundleID    string `json:"bundleId"`
		Environment string `json:"environment"`
		Host        string `json:"apnsHost"`
	}
	if !decodeJSONBody(w, r, &body, 16<<10) {
		return
	}
	a.pushMu.Lock()
	old := a.cfg.Apns
	next := old
	if strings.TrimSpace(body.KeyID) != "" {
		next.KeyID = strings.TrimSpace(body.KeyID)
	}
	if strings.TrimSpace(body.TeamID) != "" {
		next.TeamID = strings.TrimSpace(body.TeamID)
	}
	if strings.TrimSpace(body.BundleID) != "" {
		next.BundleID = strings.TrimSpace(body.BundleID)
	}
	if strings.TrimSpace(body.Environment) != "" {
		next.Environment = strings.TrimSpace(body.Environment)
	}
	next.Host = strings.TrimSpace(body.Host)
	if next.Environment == "" {
		next.Environment = "sandbox"
	}
	if next.Environment != "sandbox" && next.Environment != "production" {
		a.pushMu.Unlock()
		jsonReply(w, http.StatusBadRequest, object{"error": "environment must be sandbox or production"})
		return
	}
	a.cfg.Apns = next
	if err := saveConfig(a.cfg); err != nil {
		a.cfg.Apns = old
		a.pushMu.Unlock()
		jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
		return
	}
	a.pushMu.Unlock()
	a.syncAPNSSink(next)
	jsonReply(w, http.StatusOK, object{"config": a.pushConfigView()})
}

func (a *app) pushKeyAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := r.ParseMultipartForm(64 << 10); err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": "upload a PEM encoded .p8 file in the key field"})
		return
	}
	file, _, err := r.FormFile("key")
	if err != nil {
		file, _, err = r.FormFile("file")
	}
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": "upload a PEM encoded .p8 file in the key field"})
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 16<<10+1))
	if err != nil || len(data) > 16<<10 {
		jsonReply(w, http.StatusBadRequest, object{"error": "apns key is too large"})
		return
	}
	if _, err := parseAPNSKey(data); err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
		return
	}
	a.pushMu.Lock()
	cfg := a.cfg.Apns
	if cfg.KeyPath == "" {
		cfg.KeyPath = filepath.Join(a.cfg.home, ".config", "boxdeck", "apns.p8")
	}
	if err := os.MkdirAll(filepath.Dir(cfg.KeyPath), 0700); err != nil {
		a.pushMu.Unlock()
		jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
		return
	}
	tmp, err := os.CreateTemp(filepath.Dir(cfg.KeyPath), ".apns-")
	if err == nil {
		_ = tmp.Chmod(0600)
		_, err = tmp.Write(data)
		if closeErr := tmp.Close(); err == nil {
			err = closeErr
		}
	}
	if err == nil {
		err = os.Rename(tmp.Name(), cfg.KeyPath)
	}
	if err != nil {
		if tmp != nil {
			_ = os.Remove(tmp.Name())
		}
		a.pushMu.Unlock()
		jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
		return
	}
	a.cfg.Apns = cfg
	if err = saveConfig(a.cfg); err != nil {
		a.pushMu.Unlock()
		jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
		return
	}
	a.pushMu.Unlock()
	a.syncAPNSSink(cfg)
	jsonReply(w, http.StatusOK, object{"config": a.pushConfigView()})
}

func (a *app) syncAPNSSink(cfg apnsConfig) {
	if a.alerts == nil {
		return
	}
	a.alerts.mu.Lock()
	defer a.alerts.mu.Unlock()
	filtered := make([]alertSink, 0, len(a.alerts.cfg.Sinks)+1)
	for _, sink := range a.alerts.cfg.Sinks {
		if strings.EqualFold(sink.Type, "apns") {
			continue
		}
		filtered = append(filtered, sink)
	}
	if cfg.enabled() {
		filtered = append(filtered, alertSink{Name: "apns", Type: "apns"})
	}
	a.alerts.cfg.Sinks = filtered
}
