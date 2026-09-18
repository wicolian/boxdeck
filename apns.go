package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const apnsPayloadLimit = 4096

type apnsConfig struct {
	KeyPath     string `json:"keyPath,omitempty"`
	KeyID       string `json:"keyId,omitempty"`
	TeamID      string `json:"teamId,omitempty"`
	BundleID    string `json:"bundleId,omitempty"`
	Environment string `json:"environment,omitempty"`
	Host        string `json:"apnsHost,omitempty"`
}

func (c apnsConfig) enabled() bool {
	return c.KeyPath != "" || c.KeyID != "" || c.TeamID != "" || c.BundleID != ""
}

func (c apnsConfig) validate() error {
	if c.KeyPath == "" || c.KeyID == "" || c.TeamID == "" || c.BundleID == "" {
		return errors.New("apns keyPath, keyId, teamId, and bundleId are required")
	}
	if c.Environment != "sandbox" && c.Environment != "production" {
		return errors.New("apns environment must be sandbox or production")
	}
	return nil
}

type pushDevice struct {
	Platform string        `json:"platform"`
	Token    string        `json:"token"`
	Name     string        `json:"name,omitempty"`
	BundleID string        `json:"bundleId,omitempty"`
	Delivery alertDelivery `json:"lastDelivery,omitempty"`
}

type pushStore struct {
	Devices []pushDevice `json:"devices"`
}

type pushRegistry struct {
	mu      sync.Mutex
	path    string
	devices []pushDevice
}

func newPushRegistry(home string) *pushRegistry {
	p := &pushRegistry{path: home + "/.local/share/boxdeck/push.json"}
	p.load()
	return p
}

func (p *pushRegistry) load() {
	b, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var stored pushStore
	if json.Unmarshal(b, &stored) != nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, device := range stored.Devices {
		if validPushPlatform(device.Platform) && device.Token != "" {
			p.devices = append(p.devices, device)
		}
	}
}

func (p *pushRegistry) persistLocked() error {
	if p.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(pushStore{Devices: p.devices}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepathDir(p.path), 0700); err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, p.path)
}

func filepathDir(path string) string {
	return filepath.Dir(path)
}

func validPushPlatform(value string) bool {
	return value == "ios" || value == "watchos"
}

func pushDeviceID(device pushDevice) string {
	sum := sha256.Sum256([]byte(device.Platform + "\x00" + device.Token))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:16]
}

func (p *pushRegistry) list() []pushDevice {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]pushDevice(nil), p.devices...)
}

func (p *pushRegistry) register(device pushDevice) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.devices {
		if p.devices[i].Platform == device.Platform && p.devices[i].Token == device.Token {
			device.Delivery = p.devices[i].Delivery
			p.devices[i] = device
			return p.persistLocked()
		}
	}
	p.devices = append(p.devices, device)
	return p.persistLocked()
}

func (p *pushRegistry) remove(id, platform, token string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, device := range p.devices {
		match := id != "" && pushDeviceID(device) == id
		match = match || (token != "" && device.Token == token && (platform == "" || device.Platform == platform))
		if match {
			p.devices = append(p.devices[:i], p.devices[i+1:]...)
			return true, p.persistLocked()
		}
	}
	return false, nil
}

func (p *pushRegistry) record(device pushDevice, delivery alertDelivery) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := range p.devices {
		if p.devices[i].Platform == device.Platform && p.devices[i].Token == device.Token {
			p.devices[i].Delivery = delivery
			return p.persistLocked()
		}
	}
	return nil
}

type apnsSender struct {
	mu         sync.Mutex
	token      string
	tokenUntil time.Time
	keyID      string
	teamID     string
	keyPath    string
	now        func() time.Time
	client     *http.Client
}

func (s *apnsSender) providerToken(cfg apnsConfig) (string, error) {
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && now().Before(s.tokenUntil) && s.keyID == cfg.KeyID && s.teamID == cfg.TeamID && s.keyPath == cfg.KeyPath {
		return s.token, nil
	}
	key, err := readAPNSKey(cfg.KeyPath)
	if err != nil {
		return "", err
	}
	header, _ := json.Marshal(map[string]string{"alg": "ES256", "kid": cfg.KeyID, "typ": "JWT"})
	claims, _ := json.Marshal(map[string]any{"iss": cfg.TeamID, "iat": now().Unix()})
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(input))
	r, ss, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign apns provider token: %w", err)
	}
	signature := make([]byte, 0, 64)
	signature = append(signature, leftPad32(r.Bytes())...)
	signature = append(signature, leftPad32(ss.Bytes())...)
	token := input + "." + base64.RawURLEncoding.EncodeToString(signature)
	s.token, s.tokenUntil = token, now().Add(50*time.Minute)
	s.keyID, s.teamID, s.keyPath = cfg.KeyID, cfg.TeamID, cfg.KeyPath
	return token, nil
}

func leftPad32(value []byte) []byte {
	result := make([]byte, 32)
	if len(value) > len(result) {
		value = value[len(value)-len(result):]
	}
	copy(result[len(result)-len(value):], value)
	return result
}

func readAPNSKey(path string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read apns key: %w", err)
	}
	return parseAPNSKey(b)
}

func parseAPNSKey(b []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(b)
	if block == nil {
		return nil, errors.New("apns key is not PEM encoded")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if ec, ok := key.(*ecdsa.PrivateKey); ok && ec.Curve == elliptic.P256() {
			return ec, nil
		}
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil || key.Curve != elliptic.P256() {
		return nil, errors.New("apns key must be an ES256 P-256 private key")
	}
	return key, nil
}

type apnsHTTPError struct {
	Status int
	Reason string
}

func (e *apnsHTTPError) Error() string {
	if e.Reason == "" {
		return fmt.Sprintf("apns returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("apns returned HTTP %d: %s", e.Status, e.Reason)
}

func isAPNSUnregistered(err error) bool {
	var apnsErr *apnsHTTPError
	return errors.As(err, &apnsErr) && apnsErr.Status == http.StatusGone
}

func (s *apnsSender) send(ctx context.Context, cfg apnsConfig, device pushDevice, alert Alert, deckURL, actionToken string) error {
	if err := cfg.validate(); err != nil {
		return err
	}
	token, err := s.providerToken(cfg)
	if err != nil {
		return err
	}
	body, err := apnsPayload(alert, deckURL, actionToken)
	if err != nil {
		return err
	}
	host := cfg.Host
	if host == "" {
		host = "https://api.sandbox.push.apple.com"
		if cfg.Environment == "production" {
			host = "https://api.push.apple.com"
		}
	}
	if !strings.Contains(host, "://") {
		host = "https://" + host
	}
	base, err := url.Parse(strings.TrimRight(host, "/"))
	if err != nil || base.Host == "" {
		return errors.New("apns host is invalid")
	}
	base.Path = strings.TrimRight(base.Path, "/") + "/3/device/" + url.PathEscape(device.Token)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, base.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "bearer "+token)
	request.Header.Set("apns-topic", cfg.BundleID)
	request.Header.Set("apns-push-type", "alert")
	request.Header.Set("apns-priority", "10")
	request.Header.Set("apns-collapse-id", alert.ID)
	request.Header.Set("Content-Type", "application/json")
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var value struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(responseBody, &value)
		return &apnsHTTPError{Status: response.StatusCode, Reason: value.Reason}
	}
	return nil
}

func apnsPayload(alert Alert, deckURL, actionToken string) ([]byte, error) {
	for limit := len(alert.Body); limit >= 0; {
		copy := alert
		copy.Body = truncateUTF8(alert.Body, limit)
		apsAlert := map[string]string{"title": copy.Title, "body": copy.Body}
		aps := map[string]any{"alert": apsAlert, "category": "BOXDECK_ALERT", "thread-id": copy.Box, "mutable-content": 1}
		if copy.Severity == "critical" {
			aps["sound"] = "default"
		}
		if copy.Severity == "critical" || copy.Severity == "warning" {
			aps["interruption-level"] = "time-sensitive"
		}
		payload := map[string]any{"aps": aps, "alert": copy, "boxdeck_alert": copy, "actions": copy.Actions, "deck_url": strings.TrimRight(deckURL, "/"), "action_token": actionToken}
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		if len(data) <= apnsPayloadLimit {
			return data, nil
		}
		if limit == 0 {
			break
		}
		limit = limit / 2
	}
	return nil, fmt.Errorf("apns payload exceeds %d bytes", apnsPayloadLimit)
}

func truncateUTF8(value string, limit int) string {
	if limit >= len(value) {
		return value
	}
	if limit <= 0 {
		return ""
	}
	value = value[:limit]
	for len(value) > 0 && value[len(value)-1]&0xc0 == 0x80 {
		value = value[:len(value)-1]
	}
	if len(value) > 0 && value[len(value)-1]&0xc0 == 0xc0 {
		value = value[:len(value)-1]
	}
	return value
}
