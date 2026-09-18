package main

import (
	"bytes"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestPairJSONCreatesRevocablePhoneToken(t *testing.T) {
	a := testApp(t)
	a.cfg.path = filepath.Join(t.TempDir(), "config.json")
	a.cfg.Host = "box.example"
	a.cfg.Port = 8108
	a.cfg.Tokens = []string{"old-token"}
	r := httptest.NewRequest(http.MethodGet, "http://box.example:8108/api/pair.json", nil)
	r.Header.Set("Authorization", "Bearer old-token")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("pair status = %d, body = %s", w.Code, w.Body.String())
	}
	var response pairResponse
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Label != "phone" || response.Token == "" || response.URL != "http://box.example:8108" {
		t.Fatalf("pair response = %+v", response)
	}
	if !tokenMatches(a.cfg.Tokens, response.Token) {
		t.Fatal("new pairing token was not accepted")
	}
	if len(a.cfg.Tokens) != 2 {
		t.Fatalf("token count = %d", len(a.cfg.Tokens))
	}
	if err := tokenCommandWithConfig(&a.cfg, []string{"revoke", response.Token[:12]}); err != nil {
		t.Fatal(err)
	}
	if tokenMatches(a.cfg.Tokens, response.Token) {
		t.Fatal("pairing token was not revocable")
	}
}

func TestPairPNGContainsKnownVector(t *testing.T) {
	data, err := pairQRCode("boxdeck://add?url=http://box:8108&token=bd_test")
	if err != nil {
		t.Fatal(err)
	}
	image, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if image.Bounds().Dx() != 57 || image.Bounds().Dy() != 57 {
		t.Fatalf("QR size = %v", image.Bounds())
	}
	codewords, err := qrCodewords([]byte("boxdeck://add?url=http://box:8108&token=bd_test"))
	if err != nil {
		t.Fatal(err)
	}
	want := [32]byte{0x0d, 0xa8, 0x4d, 0x6b, 0xca, 0x4c, 0x3e, 0xb8, 0xde, 0x04, 0xa1, 0x18, 0x54, 0xc4, 0x59, 0xf0, 0x92, 0xaa, 0x3d, 0x60, 0x19, 0xa3, 0x5c, 0x18, 0x39, 0x08, 0xb3, 0x3e, 0xe9, 0x27, 0xcf, 0x4f}
	if got := pairVectorHash(qrModules(codewords)); got != want {
		t.Fatalf("QR vector = %x, want %x", got, want)
	}
}

func TestPairJSONPersistsConfig(t *testing.T) {
	a := testApp(t)
	a.cfg.path = filepath.Join(t.TempDir(), "config.json")
	a.cfg.Tokens = []string{}
	r := httptest.NewRequest(http.MethodGet, "http://box.example:8108/api/pair.json", nil)
	w := httptest.NewRecorder()
	a.pairJSON(w, r)
	if w.Code != http.StatusOK {
		t.Fatal(w.Code)
	}
	if _, err := os.Stat(a.cfg.path); err != nil {
		t.Fatal(err)
	}
}
