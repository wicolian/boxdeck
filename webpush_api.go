package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

//go:embed sw.js
var serviceWorkerJS []byte

//go:embed manifest.webmanifest
var webManifest []byte

//go:embed icon.svg
var appIcon []byte

// webPushAPI serves the browser side of Web Push.
//
//	GET    /api/push/web             VAPID public key and every subscription on this deck
//	POST   /api/push/web/subscribe   store the PushSubscription JSON from this browser
//	DELETE /api/push/web/subscribe   forget one subscription by id or endpoint
//	POST   /api/push/web/test        send a test alert to every browser
func (a *app) webPushAPI(w http.ResponseWriter, r *http.Request) {
	if a.webPush == nil || a.webPushSender == nil {
		jsonReply(w, http.StatusServiceUnavailable, object{"error": "web push is not available"})
		return
	}
	switch r.URL.Path {
	case "/api/push/web":
		if !requireMethod(w, r, http.MethodGet) {
			return
		}
		jsonReply(w, http.StatusOK, a.webPushStatus())
	case "/api/push/web/subscribe":
		a.webPushSubscribeAPI(w, r)
	case "/api/push/web/test":
		a.webPushTestAPI(w, r)
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "push route not found"})
	}
}

func (a *app) webPushStatus() object {
	records := a.webPush.list()
	views := make([]webPushView, 0, len(records))
	for _, record := range records {
		views = append(views, webPushViewOf(record))
	}
	return object{"publicKey": a.webPushSender.keys.PublicKey, "subscriptions": views, "deckURL": a.alertDeckURL()}
}

func (a *app) webPushSubscribeAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodPost:
		var body struct {
			Subscription webPushSubscription `json:"subscription"`
			Name         string              `json:"name"`
		}
		if !decodeJSONBody(w, r, &body, 16<<10) {
			return
		}
		body.Subscription.Endpoint = strings.TrimSpace(body.Subscription.Endpoint)
		body.Name = strings.TrimSpace(body.Name)
		if len(body.Name) > 120 {
			body.Name = body.Name[:120]
		}
		if body.Name == "" {
			body.Name = browserName(r.UserAgent())
		}
		if err := body.Subscription.validate(); err != nil {
			jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
			return
		}
		record, err := a.webPush.subscribe(body.Subscription, body.Name)
		if err != nil {
			jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
			return
		}
		a.syncWebPushSink()
		jsonReply(w, http.StatusCreated, webPushViewOf(record))
	case http.MethodDelete:
		var body struct {
			ID       string `json:"id"`
			Endpoint string `json:"endpoint"`
		}
		if !decodeJSONBody(w, r, &body, 16<<10) {
			return
		}
		removed, err := a.webPush.remove(strings.TrimSpace(body.ID), strings.TrimSpace(body.Endpoint))
		if err != nil {
			jsonReply(w, http.StatusInternalServerError, object{"error": err.Error()})
			return
		}
		if !removed {
			jsonReply(w, http.StatusNotFound, object{"error": "subscription not found"})
			return
		}
		a.syncWebPushSink()
		jsonReply(w, http.StatusOK, object{"removed": true})
	default:
		w.Header().Set("Allow", "POST, DELETE")
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST or DELETE to manage a browser subscription"})
	}
}

func (a *app) webPushTestAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	records := a.webPush.list()
	if len(records) == 0 {
		jsonReply(w, http.StatusBadRequest, object{"error": "no browser subscriptions"})
		return
	}
	testAlert := Alert{ID: "webpush-test-" + fmt.Sprint(time.Now().UnixNano()), Box: a.cfg.Host, Rule: "push_test", Severity: "info", Title: "Boxdeck test alert", Body: "Browser push delivery is connected.", Link: "#/alerts", At: time.Now(), Actions: []AlertAction{}}
	results := make([]object, 0, len(records))
	for _, record := range records {
		err := a.sendWebPushRecord(r.Context(), record, testAlert, "")
		result := object{"id": record.ID, "status": "delivered"}
		if err != nil {
			result["status"], result["message"] = "failed", err.Error()
		}
		results = append(results, result)
	}
	jsonReply(w, http.StatusOK, object{"results": results})
}

// sendWebPushRecord sends one alert to one browser and records the outcome. A 404 or 410 from
// the push service means the browser unsubscribed, so the record is dropped.
func (a *app) sendWebPushRecord(ctx context.Context, record webPushRecord, alert Alert, actionToken string) error {
	if a.webPushSender == nil {
		return errors.New("web push sender is not configured")
	}
	err := a.webPushSender.send(ctx, record.subscription, alert, a.alertDeckURL(), actionToken)
	status := alertDelivery{At: time.Now(), Status: "delivered", Tries: 1}
	if err != nil {
		status.Status, status.Message = "failed", err.Error()
		if isWebPushGone(err) {
			_, _ = a.webPush.remove(record.ID, "")
			a.syncWebPushSink()
			return err
		}
	}
	_ = a.webPush.record(record.ID, status)
	return err
}

// syncWebPushSink keeps exactly one webpush sink in the alert config while browsers are
// subscribed, and none when the last browser leaves, so Delivery shows the truth.
func (a *app) syncWebPushSink() {
	if a.alerts == nil || a.webPush == nil {
		return
	}
	a.alerts.mu.Lock()
	defer a.alerts.mu.Unlock()
	filtered := make([]alertSink, 0, len(a.alerts.cfg.Sinks)+1)
	for _, sink := range a.alerts.cfg.Sinks {
		if strings.EqualFold(sink.Type, "webpush") {
			continue
		}
		filtered = append(filtered, sink)
	}
	if a.webPush.count() > 0 {
		filtered = append(filtered, alertSink{Name: "webpush", Type: "webpush"})
	}
	a.alerts.cfg.Sinks = filtered
}

// browserName turns a User-Agent into a short label such as "Chrome on macOS".
func browserName(userAgent string) string {
	browser := "Browser"
	switch {
	case strings.Contains(userAgent, "Edg/"):
		browser = "Edge"
	case strings.Contains(userAgent, "OPR/"):
		browser = "Opera"
	case strings.Contains(userAgent, "Firefox/"):
		browser = "Firefox"
	case strings.Contains(userAgent, "Chrome/"):
		browser = "Chrome"
	case strings.Contains(userAgent, "Safari/"):
		browser = "Safari"
	}
	platform := ""
	switch {
	case strings.Contains(userAgent, "iPhone"):
		platform = "iPhone"
	case strings.Contains(userAgent, "iPad"):
		platform = "iPad"
	case strings.Contains(userAgent, "Android"):
		platform = "Android"
	case strings.Contains(userAgent, "Mac OS X"):
		platform = "macOS"
	case strings.Contains(userAgent, "Windows"):
		platform = "Windows"
	case strings.Contains(userAgent, "CrOS"):
		platform = "ChromeOS"
	case strings.Contains(userAgent, "Linux"):
		platform = "Linux"
	}
	if platform == "" {
		return browser
	}
	return browser + " on " + platform
}

// pwaAsset serves the service worker, the manifest and the icon. The worker is served from the
// root so its scope covers the whole deck.
func (a *app) pwaAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "Use GET for a deck asset", http.StatusMethodNotAllowed)
		return
	}
	var body []byte
	switch r.URL.Path {
	case "/sw.js":
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Service-Worker-Allowed", "/")
		body = serviceWorkerJS
	case "/manifest.webmanifest":
		w.Header().Set("Content-Type", "application/manifest+json")
		body = webManifest
	case "/icon.svg":
		w.Header().Set("Content-Type", "image/svg+xml")
		body = appIcon
	default:
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.Method == http.MethodGet {
		_, _ = w.Write(body)
	}
}
