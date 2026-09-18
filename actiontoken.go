package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Action tokens let a notification button (ntfy on a phone or a watch) act on ONE alert for a
// limited time. They are minted per alert and delivered instead of the fleet token, so a third
// party notification service never holds a credential that can do anything else.

const actionTokenAge = 24 * time.Hour

func (a *app) mintActionToken(alertID string, now time.Time) string {
	exp := strconv.FormatInt(now.Add(actionTokenAge).Unix(), 10)
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte("action\x00" + alertID + "\x00" + exp))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return "act." + base64.RawURLEncoding.EncodeToString([]byte(alertID)) + "." + exp + "." + sig
}

// actionTokenAllows reports whether a bearer token is a valid action token for this request:
// signed, unexpired, for an alert that exists, and the request is one of that alert's own
// actions or its ack, snooze or resolve.
func (a *app) actionTokenAllows(token string, r *http.Request) bool {
	if !strings.HasPrefix(token, "act.") || a.alerts == nil || r.Method != http.MethodPost {
		return false
	}
	parts := strings.Split(token, ".")
	if len(parts) != 4 {
		return false
	}
	idBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	alertID, exp, sig := string(idBytes), parts[2], parts[3]
	expAt, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expAt {
		return false
	}
	mac := hmac.New(sha256.New, a.secret)
	mac.Write([]byte("action\x00" + alertID + "\x00" + exp))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(sig)) {
		return false
	}
	alert, ok := a.alerts.get(alertID)
	if !ok {
		return false
	}
	path := r.URL.Path
	for _, suffix := range []string{"/ack", "/snooze", "/resolve"} {
		if path == "/api/alerts/"+alertID+suffix {
			return true
		}
	}
	for _, action := range alert.Actions {
		if action.Path == path && strings.EqualFold(action.Method, r.Method) {
			return true
		}
	}
	return false
}
