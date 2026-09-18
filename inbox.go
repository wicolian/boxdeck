package main

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The local inbox lets `boxdeck alert` on the same machine post an alert without a token.
// It is not a header alone: the caller must read a 0600 secret only this user can read.

func inboxSecretPath(home string) string {
	return filepath.Join(home, ".local", "share", "boxdeck", "inbox-secret")
}

// ensureInboxSecret creates the secret on first use and returns it.
func ensureInboxSecret(home string) (string, error) {
	path := inboxSecretPath(home)
	if b, err := os.ReadFile(path); err == nil && len(bytes.TrimSpace(b)) >= 32 {
		return string(bytes.TrimSpace(b)), nil
	}
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	secret := hex.EncodeToString(raw)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		return "", err
	}
	return secret, nil
}

// isLocalInboxRequest accepts a POST /api/alerts from loopback when it carries the inbox secret.
func (a *app) isLocalInboxRequest(r *http.Request) bool {
	given := strings.TrimSpace(r.Header.Get("X-Boxdeck-Local"))
	if given == "" || a.inboxSecret == "" || len(given) != len(a.inboxSecret) {
		return false
	}
	if subtle.ConstantTimeCompare([]byte(given), []byte(a.inboxSecret)) != 1 {
		return false
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	return net.ParseIP(host).IsLoopback()
}

// alertCommand is `boxdeck alert --title ... [--body ...] [--severity info|warning|critical] [--source name]`.
func alertCommand(args []string) error {
	fs := flag.NewFlagSet("alert", flag.ContinueOnError)
	source := fs.String("source", "cli", "who is reporting (tests, ci, deploy)")
	severity := fs.String("severity", "warning", "info, warning or critical")
	title := fs.String("title", "", "one line, required")
	body := fs.String("body", "", "what to do next")
	link := fs.String("link", "", "deck hash link, for example #/agents")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*title) == "" {
		return errors.New("alert needs --title")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	cfg, err := loadConfig(configPath(home), home)
	if err != nil {
		return err
	}
	secret, err := os.ReadFile(inboxSecretPath(home))
	if err != nil {
		return fmt.Errorf("no local inbox secret yet; start boxdeck once on this machine (%w)", err)
	}
	payload, _ := json.Marshal(object{"source": *source, "severity": *severity, "title": *title, "body": *body, "link": *link})
	req, err := http.NewRequest(http.MethodPost, fmt.Sprintf("http://127.0.0.1:%d/api/alerts", cfg.Port), bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Boxdeck-Local", strings.TrimSpace(string(secret)))
	res, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("is boxdeck running on port %d? %w", cfg.Port, err)
	}
	defer res.Body.Close()
	out, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode >= 300 {
		return fmt.Errorf("deck answered %d: %s", res.StatusCode, strings.TrimSpace(string(out)))
	}
	fmt.Println(strings.TrimSpace(string(out)))
	return nil
}
