package main

import (
	"crypto/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func testApp(t *testing.T) *app {
	t.Helper()
	home := t.TempDir()
	cfg := config{Port: 8103, TTYDPort: 7682, Bind: "127.0.0.1", Host: "box", User: "test.user", Password: "long:password", FilesRoot: home, home: home, path: filepath.Join(home, "config.json"), Mirror: false}
	secret := make([]byte, 32)
	rand.Read(secret)
	a := newApp(cfg, secret)
	t.Cleanup(a.cancel)
	return a
}
func request(a *app, method, target string, auth bool) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, nil)
	if auth {
		r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}
func TestBasicAuthAndBrowserLogin(t *testing.T) {
	a := testApp(t)
	for _, tt := range []struct {
		path   string
		auth   bool
		status int
	}{{"/api/state", false, 401}, {"/", false, 302}, {"/files/test.md", false, 302}, {"/term/ws", false, 302}, {"/", true, 200}, {"/login", false, 200}} {
		w := request(a, "GET", tt.path, tt.auth)
		if w.Code != tt.status {
			t.Errorf("%s: %d", tt.path, w.Code)
		}
		if w.Header().Get("WWW-Authenticate") != "" {
			t.Fatal("browser auth dialog must never be triggered")
		}
		if w.Header().Get("Content-Disposition") != "" {
			t.Fatal("authentication must never download")
		}
	}
	for _, pair := range [][2]string{{"test.user", "long:password"}, {"bad", "long:password"}, {"test.user", "short"}, {"", ""}, {strings.Repeat("u", 1000), strings.Repeat("p", 1000)}} {
		got := credentialsEqual(pair[0], pair[1], a.cfg.User, a.cfg.Password)
		want := pair[0] == a.cfg.User && pair[1] == a.cfg.Password
		if got != want {
			t.Fatal("credential mismatch")
		}
	}
}
func TestSessionCookieAndLogout(t *testing.T) {
	a := testApp(t)
	form := url.Values{"user": {a.cfg.User}, "password": {a.cfg.Password}, "next": {"/files/report.md"}}
	r := httptest.NewRequest("POST", "/login", strings.NewReader(form.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 303 || w.Header().Get("Location") != "/files/report.md" {
		t.Fatalf("login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	c := cookies[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 2592000 {
		t.Fatal("unsafe cookie attributes")
	}
	for _, value := range []string{c.Value, c.Value + "x", a.cookieValue(time.Now().Add(-time.Hour)), a.cookieValue(time.Now().Add(31 * 24 * time.Hour))} {
		r := httptest.NewRequest("GET", "/", nil)
		r.AddCookie(&http.Cookie{Name: "boxdeck", Value: value})
		if a.authed(r) != (value == c.Value) {
			t.Fatal("cookie verification")
		}
	}
	r = httptest.NewRequest("POST", "/logout", nil)
	r.AddCookie(c)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 303 || w.Result().Cookies()[0].MaxAge != -1 {
		t.Fatal("logout did not clear cookie")
	}
}
func TestLoginErrorAndRedirects(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest("POST", "/login", strings.NewReader("user=test&password=wrong"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 || !strings.Contains(w.Body.String(), "Check the password and try again") {
		t.Fatal("missing actionable error")
	}
	for _, s := range []string{"https://evil.test/", "//evil.test/", "/\\evil.test/", "/%2f%2fevil.test/", "/login", "/logout", "/\r\nx:y"} {
		if safeNext(s) != "/" {
			t.Errorf("unsafe redirect %q", s)
		}
	}
	if safeNext("/files/a.md?x=1") != "/files/a.md?x=1" {
		t.Fatal("lost next")
	}
	r = httptest.NewRequest("POST", "/logout", nil)
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("Origin", "null")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("sandboxed file can mutate deck")
	}
}
func TestSecretPersistsPrivate(t *testing.T) {
	p := filepath.Join(t.TempDir(), "secret")
	a, err := loadSecret(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := loadSecret(p)
	if err != nil || string(a) != string(b) || len(a) != 32 {
		t.Fatal("secret persistence")
	}
	st, _ := os.Stat(p)
	if st.Mode().Perm() != 0600 {
		t.Fatal("secret permissions")
	}
}
func BenchmarkCredentialsEqual(b *testing.B) {
	for _, name := range []string{"correct", "wrong-same-length", "wrong-short"} {
		b.Run(name, func(b *testing.B) {
			u, p := "admin", "password"
			if name == "wrong-same-length" {
				u, p = "xxxxx", "xxxxxxxx"
			}
			if name == "wrong-short" {
				u, p = "x", "x"
			}
			for b.Loop() {
				credentialsEqual(u, p, "admin", "password")
			}
		})
	}
}
