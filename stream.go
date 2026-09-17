package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

type liveHub struct {
	ctx      context.Context
	sampleMu sync.Mutex
	sampler  *procSampler
	latest   liveMetrics
	mu       sync.Mutex
	subs     map[chan []byte]bool
	stop     context.CancelFunc
	interval time.Duration
}

func newLiveHub(ctx context.Context, home string) *liveHub {
	return &liveHub{ctx: ctx, sampler: newProcSampler("/proc", "/sys", home), subs: map[chan []byte]bool{}, interval: time.Second}
}
func (h *liveHub) current() (liveMetrics, []procInfo) {
	h.sampleMu.Lock()
	defer h.sampleMu.Unlock()
	if time.Since(h.sampler.at) >= h.interval {
		h.latest = h.sampler.sample(time.Now())
	}
	return h.latest, append([]procInfo{}, h.sampler.all...)
}
func (h *liveHub) subscribe() (chan []byte, func()) {
	ch := make(chan []byte, 1)
	h.mu.Lock()
	h.subs[ch] = true
	if len(h.subs) == 1 {
		ctx, cancel := context.WithCancel(h.ctx)
		h.stop = cancel
		go h.run(ctx)
	}
	h.mu.Unlock()
	once := sync.Once{}
	return ch, func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			if len(h.subs) == 0 && h.stop != nil {
				h.stop()
				h.stop = nil
			}
			h.mu.Unlock()
		})
	}
}
func (h *liveHub) run(ctx context.Context) {
	ticker := time.NewTicker(h.interval)
	defer ticker.Stop()
	for {
		m, _ := h.current()
		b, _ := json.Marshal(m)
		h.mu.Lock()
		if ctx.Err() == nil {
			for ch := range h.subs {
				select {
				case ch <- b:
				default:
					select {
					case <-ch:
					default:
					}
					ch <- b
				}
			}
		}
		h.mu.Unlock()
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (a *app) stream(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonReply(w, 405, object{"error": "Use GET for the stream"})
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-store")
	w.Header().Set("X-Accel-Buffering", "no")
	control := http.NewResponseController(w)
	ch, unsubscribe := a.live.subscribe()
	defer unsubscribe()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-a.ctx.Done():
			return
		case b := <-ch:
			control.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := fmt.Fprintf(w, "event: metrics\ndata: %s\n\n", b); err != nil {
				return
			}
			if err := control.Flush(); err != nil {
				return
			}
		}
	}
}

func (a *app) uiSettings(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonReply(w, 405, object{"error": "Settings are read-only"})
		return
	}
	// Marshal the running config, then replace every credential-bearing field.
	// This also supports fields added by the independent API branch after merge.
	b, _ := json.Marshal(a.cfg)
	var cfg map[string]any
	_ = json.Unmarshal(b, &cfg)
	cfg["password"] = "********"
	if tokens, ok := cfg["tokens"].([]any); ok {
		cfg["tokens"] = fmt.Sprintf("%d tokens", len(tokens))
	} else {
		cfg["tokens"] = "0 tokens"
	}
	if boxes, ok := cfg["boxes"].([]any); ok {
		for _, v := range boxes {
			if box, ok := v.(map[string]any); ok {
				box["token"] = "********"
			}
		}
	}
	jsonReply(w, 200, object{"config": cfg, "version": version, "update": "Download the latest release and run boxdeck install"})
}
