package main

import (
	"context"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func b64(t *testing.T, value string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(strings.ReplaceAll(value, " ", ""))
	if err != nil {
		t.Fatalf("decode %q: %v", value, err)
	}
	return b
}

// rfc8291Subscription is the user agent from RFC 8291 Appendix A.
func rfc8291Subscription(t *testing.T) (webPushSubscription, *ecdh.PrivateKey) {
	t.Helper()
	var sub webPushSubscription
	sub.Endpoint = "https://push.example.net/send/example"
	sub.Keys.P256dh = "BCVxsr7N_eNgVRqvHtD0zTZsEc6-VV-JvLexhqUzORcxaOzi6-AYWXvTBHm4bjyPjs7Vd8pZGH6SRpkNtoIAiw4"
	sub.Keys.Auth = "BTBZMqHH6r4Tts7J_aSIgg"
	clientKey, err := ecdh.P256().NewPrivateKey(b64(t, "q1dXpw3UpT5VOmu_cf_v6ih07Aems3njxI-JWgLcM94"))
	if err != nil {
		t.Fatal(err)
	}
	return sub, clientKey
}

func TestWebPushEncryptMatchesRFC8291Vector(t *testing.T) {
	sub, _ := rfc8291Subscription(t)
	serverKey, err := ecdh.P256().NewPrivateKey(b64(t, "yfWPiYE-n46HLnH0KqZOF1fJJU3MYrct3AELtAQ-oRw"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := webPushEncrypt(sub, []byte("When I grow up, I want to be a watermelon"), serverKey, b64(t, "DGv6ra1nlYgDCS1FRnbzlw"))
	if err != nil {
		t.Fatal(err)
	}
	want := b64(t, "DGv6ra1nlYgDCS1FRnbzlwAAEABBBP4z9KsN6nGRTbVYI_c7VJSPQTBtkgcy27mlmlMoZIIgDll6e3vCYLocInmYWAmS6TlzAC8wEqKK6PBru3jl7A_yl95bQpu6cVPTpK4Mqgkf1CXztLVBSt2Ks3oZwbuwXPXLWyouBWLVWGNWQexSgSxsj_Qulcy4a-fN")
	if base64.RawURLEncoding.EncodeToString(got) != base64.RawURLEncoding.EncodeToString(want) {
		t.Fatalf("ciphertext mismatch\n got %s\nwant %s", base64.RawURLEncoding.EncodeToString(got), base64.RawURLEncoding.EncodeToString(want))
	}
}

func TestWebPushDecryptRoundTrip(t *testing.T) {
	sub, clientKey := rfc8291Subscription(t)
	serverKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"title":"Agent needs you","body":"claude:1.0 is waiting"}`)
	body, err := webPushEncrypt(sub, payload, serverKey, salt)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := webPushDecrypt(clientKey, sub.authSecret(), body)
	if err != nil {
		t.Fatal(err)
	}
	if string(plain) != string(payload) {
		t.Fatalf("round trip mismatch: %q", plain)
	}
	body[len(body)-1] ^= 1
	if _, err := webPushDecrypt(clientKey, sub.authSecret(), body); err == nil {
		t.Fatal("tampered body decrypted")
	}
	if _, err := webPushEncrypt(sub, make([]byte, webPushPayloadLimit+1), serverKey, salt); err == nil {
		t.Fatal("oversized payload was accepted")
	}
}

func TestWebPushSubscriptionValidate(t *testing.T) {
	good, _ := rfc8291Subscription(t)
	if err := good.validate(); err != nil {
		t.Fatalf("valid subscription rejected: %v", err)
	}
	cases := map[string]func(*webPushSubscription){
		"http endpoint":  func(s *webPushSubscription) { s.Endpoint = "http://push.example.net/x" },
		"empty endpoint": func(s *webPushSubscription) { s.Endpoint = "" },
		"short auth":     func(s *webPushSubscription) { s.Keys.Auth = "AAAA" },
		"bad p256dh":     func(s *webPushSubscription) { s.Keys.P256dh = "BCVxsr7N" },
		"off curve": func(s *webPushSubscription) {
			s.Keys.P256dh = base64.RawURLEncoding.EncodeToString(append([]byte{4}, make([]byte, 64)...))
		},
	}
	for name, mutate := range cases {
		sub := good
		mutate(&sub)
		if err := sub.validate(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestLoadVapidKeysGeneratesAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vapid.json")
	first, err := loadVapidKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("vapid key mode = %o, want 0600", info.Mode().Perm())
	}
	if len(first.publicBytes()) != 65 || first.publicBytes()[0] != 4 {
		t.Fatalf("public key is not an uncompressed P-256 point: %d bytes", len(first.publicBytes()))
	}
	second, err := loadVapidKeys(path)
	if err != nil {
		t.Fatal(err)
	}
	if second.PublicKey != first.PublicKey || second.PrivateKey != first.PrivateKey {
		t.Fatal("reload produced a different key pair")
	}
	if second.key == nil {
		t.Fatal("reloaded key is not parsed")
	}
	if err := os.WriteFile(path, []byte("{not json"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadVapidKeys(path); err == nil {
		t.Fatal("corrupt key file was accepted")
	}
}

func TestVapidAuthorizationIsValidES256(t *testing.T) {
	keys, err := loadVapidKeys("")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 18, 12, 0, 0, 0, time.UTC)
	header, err := vapidAuthorization(keys, "https://fcm.googleapis.com/fcm/send/abc", now)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(header, "vapid t=") || !strings.Contains(header, ", k="+keys.PublicKey) {
		t.Fatalf("unexpected header %q", header)
	}
	token := strings.TrimPrefix(strings.SplitN(header, ",", 2)[0], "vapid t=")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("token has %d parts", len(parts))
	}
	var claims struct {
		Aud string `json:"aud"`
		Exp int64  `json:"exp"`
		Sub string `json:"sub"`
	}
	if err := json.Unmarshal(b64(t, parts[1]), &claims); err != nil {
		t.Fatal(err)
	}
	if claims.Aud != "https://fcm.googleapis.com" {
		t.Fatalf("aud = %q", claims.Aud)
	}
	if claims.Exp != now.Add(webPushTokenAge).Unix() || claims.Exp > now.Add(24*time.Hour).Unix() {
		t.Fatalf("exp = %d", claims.Exp)
	}
	if claims.Sub == "" {
		t.Fatal("sub is empty")
	}
	signature := b64(t, parts[2])
	if len(signature) != 64 {
		t.Fatalf("signature is %d bytes, want 64", len(signature))
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	x, y := elliptic.Unmarshal(elliptic.P256(), keys.publicBytes())
	public := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
	if !ecdsa.Verify(public, digest[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		t.Fatal("signature does not verify with the advertised public key")
	}
	if _, err := vapidAuthorization(keys, "http://push.example.net/x", now); err == nil {
		t.Fatal("http endpoint was accepted")
	}
}

func TestWebPushRegistrySealsAtRest(t *testing.T) {
	home := t.TempDir()
	secret := []byte("0123456789abcdef0123456789abcdef")
	registry := newWebPushRegistry(home, secret)
	sub, _ := rfc8291Subscription(t)
	record, err := registry.subscribe(sub, "Chrome on macOS")
	if err != nil {
		t.Fatal(err)
	}
	if record.ID != webPushSubscriptionID(sub) || record.Name != "Chrome on macOS" {
		t.Fatalf("unexpected record %+v", record)
	}
	raw, err := os.ReadFile(filepath.Join(home, ".local", "share", "boxdeck", "webpush.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), sub.Endpoint) || strings.Contains(string(raw), sub.Keys.Auth) {
		t.Fatal("subscription secrets are stored in the clear")
	}
	info, _ := os.Stat(filepath.Join(home, ".local", "share", "boxdeck", "webpush.json"))
	if info.Mode().Perm() != 0600 {
		t.Fatalf("store mode = %o", info.Mode().Perm())
	}
	again := newWebPushRegistry(home, secret)
	if got := again.list(); len(got) != 1 || got[0].subscription.Endpoint != sub.Endpoint {
		t.Fatalf("reload lost the subscription: %+v", got)
	}
	other := newWebPushRegistry(home, []byte("another secret entirely, 32 bytes!"))
	if got := other.list(); len(got) != 0 {
		t.Fatalf("a different deck secret opened the store: %+v", got)
	}
	if _, err := registry.subscribe(sub, ""); err != nil {
		t.Fatal(err)
	}
	if got := registry.list(); len(got) != 1 || got[0].Name != "Chrome on macOS" {
		t.Fatalf("resubscribe should keep one record and its name: %+v", got)
	}
	if err := registry.record(record.ID, alertDelivery{Status: "delivered", Tries: 1}); err != nil {
		t.Fatal(err)
	}
	if got := registry.list(); got[0].LastDelivery.Status != "delivered" {
		t.Fatalf("delivery not recorded: %+v", got[0].LastDelivery)
	}
	removed, err := registry.remove("", sub.Endpoint)
	if err != nil || !removed {
		t.Fatalf("remove by endpoint: %v %v", removed, err)
	}
	if registry.count() != 0 {
		t.Fatal("record remains after removal")
	}
}

func TestWebPushPayloadFitsAndTruncates(t *testing.T) {
	alert := Alert{ID: "a-1", Title: "Agent needs you", Body: strings.Repeat("x", 10000), Severity: "warning", Link: "#/agents?pane=x", Actions: agentActions("x")}
	data, err := webPushPayload(alert, "http://box:8100/", "act.token")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > webPushPayloadLimit {
		t.Fatalf("payload is %d bytes", len(data))
	}
	var message webPushMessage
	if err := json.Unmarshal(data, &message); err != nil {
		t.Fatal(err)
	}
	if message.DeckURL != "http://box:8100" || message.ActionToken != "act.token" || len(message.Actions) != 4 || message.Body == "" {
		t.Fatalf("unexpected message %+v", message)
	}
	if webPushUrgency("critical") != "high" || webPushUrgency("warning") != "normal" || webPushUrgency("info") != "low" {
		t.Fatal("urgency mapping")
	}
	if topic := webPushTopic("a-1234567890-42"); len(topic) != 32 || strings.ContainsAny(topic, "+/=") {
		t.Fatalf("topic %q", topic)
	}
}

func TestWebPushSenderPostsEncryptedRequest(t *testing.T) {
	sub, clientKey := rfc8291Subscription(t)
	var got struct {
		auth, encoding, ttl, urgency, topic string
		body                                []byte
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth, got.encoding, got.ttl, got.urgency, got.topic = r.Header.Get("Authorization"), r.Header.Get("Content-Encoding"), r.Header.Get("TTL"), r.Header.Get("Urgency"), r.Header.Get("Topic")
		got.body, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()
	sub.Endpoint = server.URL + "/send/abc"
	keys, err := loadVapidKeys("")
	if err != nil {
		t.Fatal(err)
	}
	sender := &webPushSender{keys: keys, client: server.Client()}
	alert := Alert{ID: "a-1", Title: "Box under pressure", Body: "load is high", Severity: "critical", Link: "#/boxes", Actions: []AlertAction{}}
	if err := sender.send(context.Background(), sub, alert, "http://box:8100", "act.x"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.auth, "vapid t=") || got.encoding != "aes128gcm" || got.ttl != "86400" || got.urgency != "high" || got.topic == "" {
		t.Fatalf("headers: %+v", got)
	}
	plain, err := webPushDecrypt(clientKey, sub.authSecret(), got.body)
	if err != nil {
		t.Fatal(err)
	}
	var message webPushMessage
	if err := json.Unmarshal(plain, &message); err != nil {
		t.Fatal(err)
	}
	if message.Title != "Box under pressure" || message.ActionToken != "act.x" || message.DeckURL != "http://box:8100" {
		t.Fatalf("decrypted message %+v", message)
	}
}

func TestWebPushSenderReportsGone(t *testing.T) {
	sub, _ := rfc8291Subscription(t)
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "subscription expired", http.StatusGone)
	}))
	defer server.Close()
	sub.Endpoint = server.URL + "/send/abc"
	keys, _ := loadVapidKeys("")
	sender := &webPushSender{keys: keys, client: server.Client()}
	err := sender.send(context.Background(), sub, Alert{ID: "a-2", Title: "t", Body: "b", Severity: "info"}, "http://box:8100", "")
	if !isWebPushGone(err) {
		t.Fatalf("expected a gone error, got %v", err)
	}
}

func TestBrowserName(t *testing.T) {
	cases := map[string]string{
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36":                   "Chrome on macOS",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 26_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.0 Mobile/15E148 Safari/604.1": "Safari on iPhone",
		"Mozilla/5.0 (X11; Linux x86_64; rv:143.0) Gecko/20100101 Firefox/143.0":                                                                  "Firefox on Linux",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0":           "Edge on Windows",
		"curl/8.7.1": "Browser",
	}
	for agent, want := range cases {
		if got := browserName(agent); got != want {
			t.Errorf("%q: got %q want %q", agent, got, want)
		}
	}
}

func TestWebPushAPI(t *testing.T) {
	home := t.TempDir()
	a := newTestAppAt(t, home)
	sub, _ := rfc8291Subscription(t)
	body, _ := json.Marshal(object{"subscription": sub, "name": ""})
	response := doAuthedRequest(t, a, http.MethodPost, "/api/push/web/subscribe", string(body), "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 Chrome/141.0.0.0 Safari/537.36")
	if response.Code != http.StatusCreated {
		t.Fatalf("subscribe: %d %s", response.Code, response.Body.String())
	}
	var view webPushView
	_ = json.Unmarshal(response.Body.Bytes(), &view)
	if view.Name != "Chrome on macOS" || view.Host != "push.example.net" {
		t.Fatalf("view %+v", view)
	}
	response = doAuthedRequest(t, a, http.MethodGet, "/api/push/web", "", "")
	if response.Code != http.StatusOK {
		t.Fatalf("status: %d", response.Code)
	}
	var status struct {
		PublicKey     string        `json:"publicKey"`
		Subscriptions []webPushView `json:"subscriptions"`
	}
	_ = json.Unmarshal(response.Body.Bytes(), &status)
	if status.PublicKey == "" || len(status.Subscriptions) != 1 {
		t.Fatalf("status %+v", status)
	}
	if strings.Contains(response.Body.String(), sub.Endpoint) || strings.Contains(response.Body.String(), sub.Keys.Auth) {
		t.Fatal("status leaks the subscription endpoint or keys")
	}
	found := false
	a.alerts.mu.Lock()
	for _, sink := range a.alerts.cfg.Sinks {
		if sink.Type == "webpush" {
			found = true
		}
	}
	a.alerts.mu.Unlock()
	if !found {
		t.Fatal("subscribing did not add the webpush sink")
	}
	bad, _ := json.Marshal(object{"subscription": object{"endpoint": "http://push.example.net/x", "keys": object{"p256dh": sub.Keys.P256dh, "auth": sub.Keys.Auth}}})
	if response = doAuthedRequest(t, a, http.MethodPost, "/api/push/web/subscribe", string(bad), ""); response.Code != http.StatusBadRequest {
		t.Fatalf("http endpoint accepted: %d", response.Code)
	}
	remove, _ := json.Marshal(object{"id": view.ID})
	if response = doAuthedRequest(t, a, http.MethodDelete, "/api/push/web/subscribe", string(remove), ""); response.Code != http.StatusOK {
		t.Fatalf("remove: %d %s", response.Code, response.Body.String())
	}
	if response = doAuthedRequest(t, a, http.MethodDelete, "/api/push/web/subscribe", string(remove), ""); response.Code != http.StatusNotFound {
		t.Fatalf("second remove: %d", response.Code)
	}
	if response = doAuthedRequest(t, a, http.MethodPost, "/api/push/web/test", "", ""); response.Code != http.StatusBadRequest {
		t.Fatalf("test with no subscriptions: %d", response.Code)
	}
	a.alerts.mu.Lock()
	for _, sink := range a.alerts.cfg.Sinks {
		if sink.Type == "webpush" {
			t.Fatal("webpush sink remains after the last browser left")
		}
	}
	a.alerts.mu.Unlock()
	for _, path := range []string{"/sw.js", "/manifest.webmanifest", "/icon.svg"} {
		response = doAuthedRequest(t, a, http.MethodGet, path, "", "")
		if response.Code != http.StatusOK || response.Body.Len() == 0 {
			t.Fatalf("%s: %d", path, response.Code)
		}
	}
	response = doAuthedRequest(t, a, http.MethodGet, "/sw.js", "", "")
	if response.Header().Get("Service-Worker-Allowed") != "/" || !strings.HasPrefix(response.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("sw.js headers: %v", response.Header())
	}
	unauthed := httptest.NewRecorder()
	a.ServeHTTP(unauthed, httptest.NewRequest(http.MethodGet, "/api/push/web", nil))
	if unauthed.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated status: %d", unauthed.Code)
	}
}

func newTestAppAt(t *testing.T, home string) *app {
	t.Helper()
	cfg := config{User: "u", Password: "p", Host: "box", Port: 8100, home: home, path: filepath.Join(home, ".config", "boxdeck", "config.json")}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		t.Fatal(err)
	}
	a := newApp(cfg, secret)
	t.Cleanup(a.cancel)
	if a.webPushSender == nil {
		t.Fatal("web push sender was not created")
	}
	return a
}

func doAuthedRequest(t *testing.T, a *app, method, path, body, userAgent string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.SetBasicAuth("u", "p")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Sec-Fetch-Site", "same-origin")
	if userAgent != "" {
		request.Header.Set("User-Agent", userAgent)
	}
	recorder := httptest.NewRecorder()
	a.ServeHTTP(recorder, request)
	return recorder
}
