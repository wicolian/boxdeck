package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type boxConfig struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

type boxCard struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Local  bool   `json:"local"`
	OK     bool   `json:"ok"`
	Since  string `json:"since"`
	Health object `json:"health"`
	Agents int    `json:"agents"`
	Ports  int    `json:"ports"`
}

type boxCache struct {
	mu       sync.Mutex
	at       time.Time
	value    []boxCard
	failures map[string]time.Time
}

func normalizeBoxURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("url must be an http(s) URL")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || u.Path != "" && u.Path != "/" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("url must be an http(s) URL without a path")
	}
	u.Path = ""
	u.RawPath = ""
	return strings.TrimRight(u.String(), "/"), nil
}

func (a *app) healthAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read health"})
		return
	}
	jsonReply(w, http.StatusOK, object{"ok": true, "version": version})
}

func (a *app) boxes(ctx context.Context) []boxCard {
	a.boxesMemo.mu.Lock()
	if time.Since(a.boxesMemo.at) < 5*time.Second {
		value := append([]boxCard(nil), a.boxesMemo.value...)
		a.boxesMemo.mu.Unlock()
		return value
	}
	configs := append([]boxConfig(nil), a.cfg.Boxes...)
	a.boxesMemo.mu.Unlock()

	state := a.state()
	local := boxCard{
		Name:   a.cfg.Host,
		URL:    "http://" + a.cfg.Host + ":" + strconv.Itoa(int(a.cfg.Port)),
		Local:  true,
		OK:     true,
		Since:  "",
		Health: mapObject(state["health"]),
		Agents: collectionLength(state["agents"]),
		Ports:  collectionLength(state["ports"]),
	}
	value := make([]boxCard, len(configs)+1)
	value[0] = local
	var wg sync.WaitGroup
	for i, cfg := range configs {
		wg.Add(1)
		go func(i int, cfg boxConfig) {
			defer wg.Done()
			value[i+1] = a.fetchBox(ctx, cfg)
		}(i, cfg)
	}
	wg.Wait()

	a.boxesMemo.mu.Lock()
	a.boxesMemo.value = append([]boxCard(nil), value...)
	a.boxesMemo.at = time.Now()
	a.boxesMemo.mu.Unlock()
	return value
}

func mapObject(value any) object {
	if result, ok := value.(object); ok && result != nil {
		return result
	}
	return object{}
}

func collectionLength(value any) int {
	switch value := value.(type) {
	case []object:
		return len(value)
	case []portInfo:
		return len(value)
	case []agentInfo:
		return len(value)
	case []any:
		return len(value)
	default:
		return 0
	}
}

func (a *app) fetchBox(ctx context.Context, cfg boxConfig) boxCard {
	card := boxCard{Name: cfg.Name, URL: cfg.URL, Health: object{}}
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodGet, strings.TrimRight(cfg.URL, "/")+"/api/state", nil)
	if err == nil && cfg.Token != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	var state struct {
		Health object            `json:"health"`
		Agents []json.RawMessage `json:"agents"`
		Ports  []json.RawMessage `json:"ports"`
	}
	if err == nil {
		response, requestErr := (&http.Client{}).Do(req)
		if requestErr == nil {
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				err = fmt.Errorf("remote returned HTTP %d", response.StatusCode)
			} else {
				decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
				err = decoder.Decode(&state)
			}
		} else {
			err = requestErr
		}
	}
	if err != nil {
		a.boxesMemo.mu.Lock()
		if a.boxesMemo.failures == nil {
			a.boxesMemo.failures = map[string]time.Time{}
		}
		failure, ok := a.boxesMemo.failures[cfg.URL]
		if !ok {
			failure = time.Now().UTC()
			a.boxesMemo.failures[cfg.URL] = failure
		}
		a.boxesMemo.mu.Unlock()
		card.Since = failure.Format(time.RFC3339)
		return card
	}
	a.boxesMemo.mu.Lock()
	delete(a.boxesMemo.failures, cfg.URL)
	a.boxesMemo.mu.Unlock()
	card.OK = true
	card.Health = state.Health
	if card.Health == nil {
		card.Health = object{}
	}
	card.Agents = len(state.Agents)
	card.Ports = len(state.Ports)
	return card
}
