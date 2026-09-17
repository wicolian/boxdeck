package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const sessionAge = 30 * 24 * time.Hour

// Hash first so comparisons have fixed width, including different-length credentials.
// Both comparisons execute even when the username is wrong.
func credentialsEqual(user, password, wantUser, wantPassword string) bool {
	u, p, wu, wp := sha256.Sum256([]byte(user)), sha256.Sum256([]byte(password)), sha256.Sum256([]byte(wantUser)), sha256.Sum256([]byte(wantPassword))
	return subtle.ConstantTimeCompare(u[:], wu[:])&subtle.ConstantTimeCompare(p[:], wp[:]) == 1
}
func loadSecret(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("session secret must contain 32 bytes")
		}
		if err = os.Chmod(path, 0600); err != nil {
			return nil, err
		}
		return b, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return loadSecret(path)
	}
	if err != nil {
		return nil, err
	}
	_, err = f.Write(b)
	closeErr := f.Close()
	if err != nil {
		return nil, err
	}
	return b, closeErr
}
func (a *app) sign(value string) string {
	h := hmac.New(sha256.New, a.secret)
	h.Write([]byte(value))
	return hex.EncodeToString(h.Sum(nil))
}
func (a *app) cookieValue(expiry time.Time) string {
	// QueryEscape preserves ordinary usernames and safely encodes cookie delimiters.
	value := url.QueryEscape(a.cfg.User) + "." + strconv.FormatInt(expiry.Unix(), 10)
	return value + "." + a.sign(value)
}
func (a *app) authed(r *http.Request) bool {
	if u, p, ok := r.BasicAuth(); ok && credentialsEqual(u, p, a.cfg.User, a.cfg.Password) {
		return true
	}
	if a.tokenAuthed(r) {
		return true
	}
	c, err := r.Cookie("boxdeck")
	if err != nil {
		return false
	}
	sigAt := strings.LastIndex(c.Value, ".")
	if sigAt < 0 {
		return false
	}
	value, sig := c.Value[:sigAt], c.Value[sigAt+1:]
	expiryAt := strings.LastIndex(value, ".")
	if expiryAt < 0 {
		return false
	}
	user, err := url.QueryUnescape(value[:expiryAt])
	if err != nil {
		return false
	}
	expiry, err := strconv.ParseInt(value[expiryAt+1:], 10, 64)
	if err != nil || expiry <= time.Now().Unix() || expiry > time.Now().Add(sessionAge+time.Minute).Unix() {
		return false
	}
	got, err := hex.DecodeString(sig)
	if err != nil {
		return false
	}
	want, _ := hex.DecodeString(a.sign(value))
	return subtle.ConstantTimeCompare(got, want) == 1 && credentialsEqual(user, "", a.cfg.User, "")
}
func safeNext(next string) string {
	if !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.ContainsAny(next, "\\\r\n") {
		return "/"
	}
	u, err := url.Parse(next)
	if err != nil || u.IsAbs() || u.Host != "" || strings.HasPrefix(u.Path, "//") || strings.ContainsAny(u.Path, "\\\r\n") || u.Path == "/login" || u.Path == "/logout" {
		return "/"
	}
	return next
}
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return r.Header.Get("Sec-Fetch-Site") != "cross-site"
	}
	u, err := url.Parse(origin)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && strings.EqualFold(u.Host, r.Host)
}
func jsonReply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = jsonEncode(w, v)
}
func (a *app) unauthorized(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") {
		jsonReply(w, 401, map[string]string{"error": "Sign in to continue", "login": "/login"})
		return
	}
	http.Redirect(w, r, "/login?next="+url.QueryEscape(safeNext(r.URL.RequestURI())), 302)
}
func (a *app) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'")
	next := safeNext(r.URL.Query().Get("next"))
	message := ""
	if r.Method == http.MethodPost {
		if !sameOrigin(r) {
			http.Error(w, "Open the deck and sign in again", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 8192)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Check the form and try again", 400)
			return
		}
		next = safeNext(r.Form.Get("next"))
		if credentialsEqual(r.Form.Get("user"), r.Form.Get("password"), a.cfg.User, a.cfg.Password) {
			expiry := time.Now().Add(sessionAge)
			http.SetCookie(w, &http.Cookie{Name: "boxdeck", Value: a.cookieValue(expiry), Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil, Expires: expiry, MaxAge: int(sessionAge.Seconds())})
			http.Redirect(w, r, next, 303)
			return
		}
		message = "Check the password and try again"
		w.WriteHeader(401)
	} else if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "Use the sign-in form", 405)
		return
	}
	_ = loginTemplate.Execute(w, map[string]any{"Title": a.cfg.Title, "Next": next, "Error": message, "Style": template.CSS(pageStyle())})
}

var loginTemplate = template.Must(template.New("login").Parse(`<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Sign in / {{.Title}}</title><style>{{.Style}}
.login{max-width:380px;margin:14vh auto;padding:24px}.login h1{font-size:20px;margin:0 0 8px}.login p{color:var(--ink-2)}.login label{display:block;margin:20px 0 8px}.login input{width:100%;min-height:48px;padding:12px;border:1px solid var(--seam-hard);border-radius:6px;background:var(--plate);color:var(--ink);font:inherit}.login button{width:100%;min-height:48px;margin-top:24px}.login .error{color:var(--lamp);min-height:24px}@media(max-width:820px){.login input,.login button{min-height:56px}}</style><main class="login"><h1>{{.Title}}</h1><p>One sign-in for your deck, files and terminal.</p><form method="post" action="/login"><input type="hidden" name="next" value="{{.Next}}"><label for="user">User</label><input id="user" name="user" autocomplete="username" required autofocus><label for="password">Password</label><input id="password" name="password" type="password" autocomplete="current-password" required><p class="error" role="alert">{{.Error}}</p><button type="submit">Sign in</button></form></main></html>`))
