package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
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

func testAPNSKey(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "AuthKey.p8")
	if err := os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestAPNSProviderJWTUsesES256AndCaches(t *testing.T) {
	path := testAPNSKey(t)
	now := time.Date(2026, 9, 18, 1, 0, 0, 0, time.UTC)
	sender := &apnsSender{now: func() time.Time { return now }}
	token, err := sender.providerToken(apnsConfig{KeyPath: path, KeyID: "ABC123", TeamID: "TEAMID", BundleID: "dev.wicolian.boxdeck", Environment: "sandbox"})
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts = %d", len(parts))
	}
	var header map[string]string
	if data, err := base64.RawURLEncoding.DecodeString(parts[0]); err != nil || json.Unmarshal(data, &header) != nil || header["alg"] != "ES256" || header["kid"] != "ABC123" {
		t.Fatalf("jwt header = %s", parts[0])
	}
	var claims map[string]any
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || json.Unmarshal(data, &claims) != nil || claims["iss"] != "TEAMID" {
		t.Fatalf("jwt claims = %s", data)
	}
	keyBytes, _ := os.ReadFile(path)
	block, _ := pem.Decode(keyBytes)
	parsed, _ := x509.ParsePKCS8PrivateKey(block.Bytes)
	key := parsed.(*ecdsa.PrivateKey)
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != 64 {
		t.Fatalf("jwt signature = %x", signature)
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(&key.PublicKey, digest[:], newBigInt(signature[:32]), newBigInt(signature[32:])) {
		t.Fatal("jwt signature did not verify")
	}
	cached, err := sender.providerToken(apnsConfig{KeyPath: path, KeyID: "ABC123", TeamID: "TEAMID", BundleID: "dev.wicolian.boxdeck", Environment: "sandbox"})
	if err != nil || cached != token {
		t.Fatalf("cached token = %q, %v", cached, err)
	}
}

func newBigInt(value []byte) *big.Int {
	return new(big.Int).SetBytes(value)
}

func TestAPNSPayloadAndRequestShape(t *testing.T) {
	path := testAPNSKey(t)
	var requestBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	sender := &apnsSender{client: server.Client()}
	cfg := apnsConfig{KeyPath: path, KeyID: "ABC123", TeamID: "TEAMID", BundleID: "dev.wicolian.boxdeck", Environment: "sandbox", Host: server.URL}
	alert := Alert{ID: "a1", Box: "box", Rule: "agent_needs_you", Severity: "critical", Title: "Needs you", Body: strings.Repeat("body ", 2000), Actions: agentActions("qa")}
	if err := sender.send(t.Context(), cfg, pushDevice{Platform: "ios", Token: strings.Repeat("a", 64)}, alert, "http://box:8100", "act.token"); err != nil {
		t.Fatal(err)
	}
	if len(requestBody) > apnsPayloadLimit {
		t.Fatalf("payload size = %d", len(requestBody))
	}
	var payload map[string]any
	if err := json.Unmarshal(requestBody, &payload); err != nil {
		t.Fatal(err)
	}
	aps := payload["aps"].(map[string]any)
	if aps["category"] != "BOXDECK_ALERT" || aps["sound"] != "default" || aps["interruption-level"] != "time-sensitive" {
		t.Fatalf("aps = %#v", aps)
	}
	if payload["action_token"] != "act.token" || payload["deck_url"] != "http://box:8100" {
		t.Fatalf("custom payload = %#v", payload)
	}
}

func TestAPNS410RemovesDevice(t *testing.T) {
	path := testAPNSKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGone)
		_, _ = w.Write([]byte(`{"reason":"Unregistered"}`))
	}))
	defer server.Close()
	registry := newPushRegistry(t.TempDir())
	device := pushDevice{Platform: "ios", Token: "deadbeef", Name: "phone"}
	if err := registry.register(device); err != nil {
		t.Fatal(err)
	}
	m := alertTestManager(t)
	m.push = registry
	sender := &apnsSender{client: server.Client()}
	m.sendAPNS = func(ctx context.Context, device pushDevice, alert Alert) error {
		return sender.send(ctx, apnsConfig{KeyPath: path, KeyID: "ABC123", TeamID: "TEAMID", BundleID: "dev.wicolian.boxdeck", Environment: "sandbox", Host: server.URL}, device, alert, "http://box:8100", "")
	}
	if err := m.deliverOne(context.Background(), alertSink{Name: "apns", Type: "apns"}, Alert{ID: "a1", Box: "box", Severity: "critical", Title: "Alert"}); err == nil {
		t.Fatal("410 delivery unexpectedly passed")
	}
	if got := registry.list(); len(got) != 0 {
		t.Fatalf("unregistered devices = %+v", got)
	}
}

func TestPushRegistrationStoreAndAPI(t *testing.T) {
	a := testApp(t)
	body := strings.NewReader(`{"platform":"ios","token":"0123456789abcdef","name":"Koushik phone","bundleId":"dev.wicolian.boxdeck"}`)
	r := httptest.NewRequest(http.MethodPost, "/api/push/register", body)
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusCreated {
		t.Fatalf("register = %d %s", w.Code, w.Body.String())
	}
	var device pushDeviceView
	if err := json.Unmarshal(w.Body.Bytes(), &device); err != nil || device.TokenSuffix != "cdef" || strings.Contains(w.Body.String(), "0123456789abcdef") {
		t.Fatalf("device response = %s", w.Body.String())
	}
	storePath := filepath.Join(a.cfg.home, ".local", "share", "boxdeck", "push.json")
	info, err := os.Stat(storePath)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatalf("push store = %v, %v", info, err)
	}
	got := request(a, http.MethodGet, "/api/push", true)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), device.ID) {
		t.Fatalf("push list = %d %s", got.Code, got.Body.String())
	}
	deleteBody := strings.NewReader(`{"id":"` + device.ID + `"}`)
	r = httptest.NewRequest(http.MethodDelete, "/api/push/register", deleteBody)
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK || len(a.push.list()) != 0 {
		t.Fatalf("delete = %d %s devices=%+v", w.Code, w.Body.String(), a.push.list())
	}
}
