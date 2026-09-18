package main

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hkdf"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
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

// Web Push for the deck PWA. The browser subscribes through its push service (RFC 8030), the
// deck signs each request with a VAPID key it generated on first run (RFC 8292), and the alert
// payload is encrypted for that one subscription with aes128gcm (RFC 8291, RFC 8188). Only the
// Go standard library is used.

const (
	webPushRecordSize   = 4096
	webPushPayloadLimit = webPushRecordSize - 16 - 1 - 86 // tag, delimiter, header
	webPushTTL          = 24 * time.Hour
	webPushTokenAge     = 12 * time.Hour
	webPushSubject      = "https://github.com/wicolian/boxdeck"
)

// vapidKeys is the persisted application server key pair.
type vapidKeys struct {
	PrivateKey string    `json:"privateKey"`
	PublicKey  string    `json:"publicKey"`
	CreatedAt  time.Time `json:"createdAt"`
	key        *ecdsa.PrivateKey
}

func (k vapidKeys) publicBytes() []byte {
	b, _ := base64.RawURLEncoding.DecodeString(k.PublicKey)
	return b
}

// loadVapidKeys reads the key pair at path or generates and writes one with mode 0600.
func loadVapidKeys(path string) (vapidKeys, error) {
	var keys vapidKeys
	if b, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(b, &keys); err != nil {
			return keys, fmt.Errorf("read vapid keys: %w", err)
		}
		der, err := base64.RawURLEncoding.DecodeString(keys.PrivateKey)
		if err != nil {
			return keys, fmt.Errorf("read vapid keys: %w", err)
		}
		parsed, err := x509.ParsePKCS8PrivateKey(der)
		if err != nil {
			return keys, fmt.Errorf("read vapid keys: %w", err)
		}
		key, ok := parsed.(*ecdsa.PrivateKey)
		if !ok || key.Curve != elliptic.P256() {
			return keys, errors.New("vapid key must be a P-256 private key")
		}
		keys.key = key
		if keys.PublicKey == "" {
			pub, err := key.PublicKey.ECDH()
			if err != nil {
				return keys, err
			}
			keys.PublicKey = base64.RawURLEncoding.EncodeToString(pub.Bytes())
		}
		return keys, nil
	} else if !os.IsNotExist(err) {
		return keys, err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return keys, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return keys, err
	}
	pub, err := key.PublicKey.ECDH()
	if err != nil {
		return keys, err
	}
	keys = vapidKeys{PrivateKey: base64.RawURLEncoding.EncodeToString(der), PublicKey: base64.RawURLEncoding.EncodeToString(pub.Bytes()), CreatedAt: time.Now().UTC(), key: key}
	if path == "" {
		return keys, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return keys, err
	}
	data, _ := json.MarshalIndent(keys, "", "  ")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if os.IsExist(err) {
		return loadVapidKeys(path)
	}
	if err != nil {
		return keys, err
	}
	_, err = f.Write(append(data, '\n'))
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	return keys, err
}

// vapidAuthorization builds the Authorization header for one push service origin.
func vapidAuthorization(keys vapidKeys, endpoint string, now time.Time) (string, error) {
	if keys.key == nil {
		return "", errors.New("vapid key is not loaded")
	}
	target, err := url.Parse(endpoint)
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return "", errors.New("push endpoint must be an https URL")
	}
	header, _ := json.Marshal(map[string]string{"typ": "JWT", "alg": "ES256"})
	claims, _ := json.Marshal(map[string]any{"aud": target.Scheme + "://" + target.Host, "exp": now.Add(webPushTokenAge).Unix(), "sub": webPushSubject})
	input := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(input))
	r, s, err := ecdsa.Sign(rand.Reader, keys.key, digest[:])
	if err != nil {
		return "", fmt.Errorf("sign vapid token: %w", err)
	}
	signature := append(leftPad32(r.Bytes()), leftPad32(s.Bytes())...)
	return "vapid t=" + input + "." + base64.RawURLEncoding.EncodeToString(signature) + ", k=" + keys.PublicKey, nil
}

// webPushSubscription is the PushSubscription JSON a browser hands to the deck.
type webPushSubscription struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	ExpirationTime *float64 `json:"expirationTime,omitempty"`
}

func (s webPushSubscription) validate() error {
	target, err := url.Parse(strings.TrimSpace(s.Endpoint))
	if err != nil || target.Scheme != "https" || target.Host == "" {
		return errors.New("subscription endpoint must be an https URL")
	}
	if len(s.Endpoint) > 2048 || strings.ContainsAny(s.Endpoint, "\r\n ") {
		return errors.New("subscription endpoint is invalid")
	}
	pub, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	if err != nil || len(pub) != 65 || pub[0] != 4 {
		return errors.New("subscription p256dh key must be a 65 byte uncompressed P-256 point")
	}
	if _, err := ecdh.P256().NewPublicKey(pub); err != nil {
		return errors.New("subscription p256dh key is not on the curve")
	}
	auth, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	if err != nil || len(auth) != 16 {
		return errors.New("subscription auth secret must be 16 bytes")
	}
	return nil
}

func (s webPushSubscription) clientKey() []byte {
	b, _ := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.P256dh, "="))
	return b
}

func (s webPushSubscription) authSecret() []byte {
	b, _ := base64.RawURLEncoding.DecodeString(strings.TrimRight(s.Keys.Auth, "="))
	return b
}

// webPushEncrypt seals plaintext for the subscription with aes128gcm content encoding. The
// ephemeral key and salt are parameters so the RFC 8291 test vector can be checked exactly.
func webPushEncrypt(sub webPushSubscription, plaintext []byte, serverKey *ecdh.PrivateKey, salt []byte) ([]byte, error) {
	if len(plaintext) > webPushPayloadLimit {
		return nil, fmt.Errorf("web push payload exceeds %d bytes", webPushPayloadLimit)
	}
	if len(salt) != 16 {
		return nil, errors.New("web push salt must be 16 bytes")
	}
	clientPublic, err := ecdh.P256().NewPublicKey(sub.clientKey())
	if err != nil {
		return nil, fmt.Errorf("client key: %w", err)
	}
	shared, err := serverKey.ECDH(clientPublic)
	if err != nil {
		return nil, fmt.Errorf("ecdh: %w", err)
	}
	serverPublic := serverKey.PublicKey().Bytes()
	keyInfo := "WebPush: info\x00" + string(clientPublic.Bytes()) + string(serverPublic)
	ikm, err := hkdf.Key(sha256.New, shared, sub.authSecret(), keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded := append(append([]byte(nil), plaintext...), 0x02)
	sealed := gcm.Seal(nil, nonce, padded, nil)
	var header bytes.Buffer
	header.Write(salt)
	_ = binary.Write(&header, binary.BigEndian, uint32(webPushRecordSize))
	header.WriteByte(byte(len(serverPublic)))
	header.Write(serverPublic)
	return append(header.Bytes(), sealed...), nil
}

// webPushDecrypt is the receiver side of webPushEncrypt. The deck never needs it at run time,
// but tests use it to prove a browser could open what the deck sends.
func webPushDecrypt(clientKey *ecdh.PrivateKey, authSecret, body []byte) ([]byte, error) {
	if len(body) < 21 {
		return nil, errors.New("web push body is too short")
	}
	salt, idLen := body[:16], int(body[20])
	if len(body) < 21+idLen {
		return nil, errors.New("web push header is truncated")
	}
	serverPublicBytes := body[21 : 21+idLen]
	serverPublic, err := ecdh.P256().NewPublicKey(serverPublicBytes)
	if err != nil {
		return nil, err
	}
	shared, err := clientKey.ECDH(serverPublic)
	if err != nil {
		return nil, err
	}
	keyInfo := "WebPush: info\x00" + string(clientKey.PublicKey().Bytes()) + string(serverPublicBytes)
	ikm, err := hkdf.Key(sha256.New, shared, authSecret, keyInfo, 32)
	if err != nil {
		return nil, err
	}
	prk, err := hkdf.Extract(sha256.New, ikm, salt)
	if err != nil {
		return nil, err
	}
	cek, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: aes128gcm\x00", 16)
	if err != nil {
		return nil, err
	}
	nonce, err := hkdf.Expand(sha256.New, prk, "Content-Encoding: nonce\x00", 12)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(cek)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	padded, err := gcm.Open(nil, nonce, body[21+idLen:], nil)
	if err != nil {
		return nil, err
	}
	end := len(padded) - 1
	for end >= 0 && padded[end] == 0 {
		end--
	}
	if end < 0 || padded[end] != 0x02 && padded[end] != 0x01 {
		return nil, errors.New("web push record has no delimiter")
	}
	return padded[:end], nil
}

// webPushRecord is one stored subscription. The endpoint and keys are sealed at rest with a
// key derived from the deck secret, so the data directory alone cannot send to the browser.
type webPushRecord struct {
	ID           string        `json:"id"`
	Name         string        `json:"name,omitempty"`
	Sealed       string        `json:"sealed"`
	CreatedAt    time.Time     `json:"createdAt"`
	LastDelivery alertDelivery `json:"lastDelivery,omitempty"`
	subscription webPushSubscription
}

type webPushStore struct {
	Subscriptions []webPushRecord `json:"subscriptions"`
}

type webPushRegistry struct {
	mu      sync.Mutex
	path    string
	sealKey []byte
	records []webPushRecord
}

func webPushSealKey(secret []byte) []byte {
	sum := sha256.Sum256(append([]byte("boxdeck webpush\x00"), secret...))
	return sum[:]
}

func newWebPushRegistry(home string, secret []byte) *webPushRegistry {
	r := &webPushRegistry{path: filepath.Join(home, ".local", "share", "boxdeck", "webpush.json"), sealKey: webPushSealKey(secret)}
	r.load()
	return r
}

func webPushSubscriptionID(sub webPushSubscription) string {
	sum := sha256.Sum256([]byte(sub.Endpoint))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:16]
}

func (r *webPushRegistry) seal(sub webPushSubscription) (string, error) {
	plain, err := json.Marshal(sub)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(r.sealKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(append(nonce, gcm.Seal(nil, nonce, plain, nil)...)), nil
}

func (r *webPushRegistry) open(sealed string) (webPushSubscription, error) {
	var sub webPushSubscription
	data, err := base64.RawURLEncoding.DecodeString(sealed)
	if err != nil {
		return sub, err
	}
	block, err := aes.NewCipher(r.sealKey)
	if err != nil {
		return sub, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return sub, err
	}
	if len(data) < gcm.NonceSize() {
		return sub, errors.New("sealed subscription is too short")
	}
	plain, err := gcm.Open(nil, data[:gcm.NonceSize()], data[gcm.NonceSize():], nil)
	if err != nil {
		return sub, err
	}
	err = json.Unmarshal(plain, &sub)
	return sub, err
}

func (r *webPushRegistry) load() {
	b, err := os.ReadFile(r.path)
	if err != nil {
		return
	}
	var stored webPushStore
	if json.Unmarshal(b, &stored) != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, record := range stored.Subscriptions {
		sub, err := r.open(record.Sealed)
		if err != nil || sub.validate() != nil {
			continue
		}
		record.subscription = sub
		if record.ID == "" {
			record.ID = webPushSubscriptionID(sub)
		}
		r.records = append(r.records, record)
	}
}

func (r *webPushRegistry) persistLocked() error {
	if r.path == "" {
		return nil
	}
	data, err := json.MarshalIndent(webPushStore{Subscriptions: r.records}, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(r.path), 0700); err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0600); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *webPushRegistry) list() []webPushRecord {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]webPushRecord(nil), r.records...)
}

func (r *webPushRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.records)
}

// subscribe stores or refreshes a subscription and returns its record.
func (r *webPushRegistry) subscribe(sub webPushSubscription, name string) (webPushRecord, error) {
	if err := sub.validate(); err != nil {
		return webPushRecord{}, err
	}
	sealed, err := r.seal(sub)
	if err != nil {
		return webPushRecord{}, err
	}
	record := webPushRecord{ID: webPushSubscriptionID(sub), Name: name, Sealed: sealed, CreatedAt: time.Now().UTC(), subscription: sub}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.records {
		if r.records[i].ID == record.ID {
			record.CreatedAt, record.LastDelivery = r.records[i].CreatedAt, r.records[i].LastDelivery
			if record.Name == "" {
				record.Name = r.records[i].Name
			}
			r.records[i] = record
			return record, r.persistLocked()
		}
	}
	r.records = append(r.records, record)
	return record, r.persistLocked()
}

func (r *webPushRegistry) remove(id, endpoint string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, record := range r.records {
		if id != "" && record.ID == id || endpoint != "" && record.subscription.Endpoint == endpoint {
			r.records = append(r.records[:i], r.records[i+1:]...)
			return true, r.persistLocked()
		}
	}
	return false, nil
}

func (r *webPushRegistry) record(id string, delivery alertDelivery) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range r.records {
		if r.records[i].ID == id {
			r.records[i].LastDelivery = delivery
			return r.persistLocked()
		}
	}
	return nil
}

type webPushView struct {
	ID           string        `json:"id"`
	Name         string        `json:"name,omitempty"`
	Host         string        `json:"host"`
	CreatedAt    time.Time     `json:"createdAt"`
	LastDelivery alertDelivery `json:"lastDelivery,omitempty"`
}

func webPushViewOf(record webPushRecord) webPushView {
	host := ""
	if target, err := url.Parse(record.subscription.Endpoint); err == nil {
		host = target.Host
	}
	return webPushView{ID: record.ID, Name: record.Name, Host: host, CreatedAt: record.CreatedAt, LastDelivery: record.LastDelivery}
}

// webPushHTTPError carries the push service status so 404 and 410 can drop a dead subscription.
type webPushHTTPError struct {
	Status int
	Body   string
}

func (e *webPushHTTPError) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("push service returned HTTP %d", e.Status)
	}
	return fmt.Sprintf("push service returned HTTP %d: %s", e.Status, e.Body)
}

func isWebPushGone(err error) bool {
	var httpErr *webPushHTTPError
	return errors.As(err, &httpErr) && (httpErr.Status == http.StatusGone || httpErr.Status == http.StatusNotFound)
}

// webPushMessage is what the service worker receives. Actions carry the per alert action token,
// never a fleet or session credential.
type webPushMessage struct {
	ID          string        `json:"id"`
	Title       string        `json:"title"`
	Body        string        `json:"body"`
	Severity    string        `json:"severity"`
	Rule        string        `json:"rule"`
	Box         string        `json:"box"`
	Link        string        `json:"link"`
	At          time.Time     `json:"at"`
	Count       int           `json:"count,omitempty"`
	DeckURL     string        `json:"deckURL"`
	ActionToken string        `json:"actionToken,omitempty"`
	Actions     []AlertAction `json:"actions"`
}

func webPushPayload(alert Alert, deckURL, actionToken string) ([]byte, error) {
	for limit := len(alert.Body); limit >= 0; {
		message := webPushMessage{ID: alert.ID, Title: alert.Title, Body: truncateUTF8(alert.Body, limit), Severity: alert.Severity, Rule: alert.Rule, Box: alert.Box, Link: alert.Link, At: alert.At, Count: alert.Count, DeckURL: strings.TrimRight(deckURL, "/"), ActionToken: actionToken, Actions: alert.Actions}
		if message.Actions == nil {
			message.Actions = []AlertAction{}
		}
		data, err := json.Marshal(message)
		if err != nil {
			return nil, err
		}
		if len(data) <= webPushPayloadLimit {
			return data, nil
		}
		if limit == 0 {
			break
		}
		limit = limit / 2
	}
	return nil, fmt.Errorf("web push payload exceeds %d bytes", webPushPayloadLimit)
}

func webPushUrgency(severity string) string {
	switch severity {
	case "critical":
		return "high"
	case "warning":
		return "normal"
	default:
		return "low"
	}
}

// webPushTopic collapses repeated sends for one alert. Topics are at most 32 URL safe characters.
func webPushTopic(alertID string) string {
	sum := sha256.Sum256([]byte(alertID))
	return base64.RawURLEncoding.EncodeToString(sum[:])[:32]
}

type webPushSender struct {
	keys   vapidKeys
	client *http.Client
	now    func() time.Time
}

func (s *webPushSender) send(ctx context.Context, sub webPushSubscription, alert Alert, deckURL, actionToken string) error {
	if err := sub.validate(); err != nil {
		return err
	}
	payload, err := webPushPayload(alert, deckURL, actionToken)
	if err != nil {
		return err
	}
	serverKey, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return err
	}
	body, err := webPushEncrypt(sub, payload, serverKey, salt)
	if err != nil {
		return err
	}
	now := time.Now
	if s.now != nil {
		now = s.now
	}
	authorization, err := vapidAuthorization(s.keys, sub.Endpoint, now())
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, sub.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", authorization)
	request.Header.Set("Content-Type", "application/octet-stream")
	request.Header.Set("Content-Encoding", "aes128gcm")
	request.Header.Set("TTL", fmt.Sprint(int(webPushTTL.Seconds())))
	request.Header.Set("Urgency", webPushUrgency(alert.Severity))
	if alert.ID != "" {
		request.Header.Set("Topic", webPushTopic(alert.ID))
	}
	client := s.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	responseBody, _ := io.ReadAll(io.LimitReader(response.Body, 1024))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return &webPushHTTPError{Status: response.StatusCode, Body: strings.TrimSpace(string(responseBody))}
	}
	return nil
}
