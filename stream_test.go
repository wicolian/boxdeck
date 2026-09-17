package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestStreamStopsSamplingWithoutSubscribers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h := newLiveHub(ctx, "")
	h.sampler = fixtureSampler(t)
	h.interval = 15 * time.Millisecond
	ch, stop := h.subscribe()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no initial sample")
	}
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("no live sample")
	}
	stop()
	time.Sleep(30 * time.Millisecond)
	h.sampleMu.Lock()
	at := h.sampler.at
	h.sampleMu.Unlock()
	time.Sleep(50 * time.Millisecond)
	h.sampleMu.Lock()
	after := h.sampler.at
	h.sampleMu.Unlock()
	if !at.Equal(after) {
		t.Fatal("sampler kept running while idle")
	}
	h.mu.Lock()
	count := len(h.subs)
	h.mu.Unlock()
	if count != 0 {
		t.Fatal("subscriber leaked")
	}
}
func TestStreamHTTPAndCancellation(t *testing.T) {
	a := testApp(t)
	a.live.sampler = fixtureSampler(t)
	server := httptest.NewServer(a)
	defer server.Close()
	client := http.Client{Timeout: time.Second}
	r, _ := http.NewRequest("GET", server.URL+"/api/stream", nil)
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	res, err := client.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatal("not SSE")
	}
	reader := bufio.NewReader(res.Body)
	found := false
	for i := 0; i < 4; i++ {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(line, "data: ") {
			var m liveMetrics
			if err = json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
				t.Fatal(err)
			}
			if len(m.Cores) != 2 {
				t.Fatal("missing cores")
			}
			found = true
			break
		}
	}
	res.Body.Close()
	if !found {
		t.Fatal("no data event")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		a.live.mu.Lock()
		count := len(a.live.subs)
		a.live.mu.Unlock()
		if count == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("disconnect left sampler active")
}
func TestUISettingsMasksPassword(t *testing.T) {
	a := testApp(t)
	w := request(a, "GET", "/api/ui/settings", true)
	if w.Code != 200 || strings.Contains(w.Body.String(), a.cfg.Password) || !strings.Contains(w.Body.String(), "0 tokens") {
		t.Fatal("settings exposed credentials")
	}
}
