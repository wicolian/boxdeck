package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

func usageDays(r *http.Request) int {
	days, err := strconv.Atoi(r.URL.Query().Get("days"))
	if err != nil || days == 0 {
		return 30
	}
	if days < 1 {
		return 1
	}
	if days > 30 {
		return 30
	}
	return days
}

func (a *app) usageAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read usage"})
		return
	}
	jsonReply(w, http.StatusOK, a.usage.snapshot(r.Context(), usageDays(r)))
}

type allUsageBox struct {
	Name       string                           `json:"name"`
	URL        string                           `json:"url"`
	Local      bool                             `json:"local"`
	OK         bool                             `json:"ok"`
	Discovered bool                             `json:"discovered,omitempty"`
	Providers  map[string]usageProviderResponse `json:"providers"`
	Error      string                           `json:"error,omitempty"`
}

type allUsageResponse struct {
	Providers map[string]usageProviderResponse `json:"providers"`
	Boxes     []allUsageBox                    `json:"boxes"`
	UpdatedAt string                           `json:"updatedAt"`
}

func (a *app) usageAllAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read all device usage"})
		return
	}
	days := usageDays(r)
	local := a.usage.snapshot(r.Context(), days)
	result := allUsageResponse{Providers: map[string]usageProviderResponse{}, Boxes: []allUsageBox{{Name: a.cfg.Host, URL: "http://" + a.cfg.Host + ":" + strconv.Itoa(int(a.cfg.Port)), Local: true, OK: true, Providers: local.Providers}}}
	addUsageResponse(&result, local)

	configs := append([]boxConfig(nil), a.cfg.Boxes...)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, cfg := range configs {
		cfg := cfg
		wg.Add(1)
		go func() {
			defer wg.Done()
			remote, err := fetchRemoteUsage(r.Context(), cfg, days)
			box := allUsageBox{Name: cfg.Name, URL: cfg.URL, Providers: map[string]usageProviderResponse{}}
			if err != nil {
				box.Error = err.Error()
				mu.Lock()
				result.Boxes = append(result.Boxes, box)
				mu.Unlock()
				return
			}
			box.OK = true
			box.Providers = remote.Providers
			mu.Lock()
			result.Boxes = append(result.Boxes, box)
			addUsageResponse(&result, remote)
			mu.Unlock()
		}()
	}
	wg.Wait()
	for _, device := range a.discoveredDevices(r.Context()) {
		if !device.Boxdeck {
			continue
		}
		box := allUsageBox{Name: device.Name, URL: device.URL, Discovered: true, Providers: map[string]usageProviderResponse{}}
		if !device.UsageOK {
			box.Error = device.Message
			result.Boxes = append(result.Boxes, box)
			continue
		}
		b, _ := json.Marshal(device.Usage)
		var remote usageResponse
		if err := json.Unmarshal(b, &remote); err != nil {
			box.Error = "invalid discovered usage response"
		} else {
			box.OK = true
			box.Providers = remote.Providers
			addUsageResponse(&result, remote)
		}
		result.Boxes = append(result.Boxes, box)
	}
	result.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	jsonReply(w, http.StatusOK, result)
}

func addUsageResponse(all *allUsageResponse, response usageResponse) {
	for provider, incoming := range response.Providers {
		current := all.Providers[provider]
		current.Today.Tokens.add(incoming.Today.Tokens)
		current.Today.CostUSD += incoming.Today.CostUSD
		current.LocalDay = incoming.LocalDay
		if current.Days == nil {
			current.Days = []usageDayResponse{}
		}
		byDay := map[string]usageDayResponse{}
		for _, day := range current.Days {
			byDay[day.Day] = day
		}
		for _, day := range incoming.Days {
			row := byDay[day.Day]
			row.Day = day.Day
			row.Tokens.add(day.Tokens)
			row.CostUSD += day.CostUSD
			if row.Models == nil {
				row.Models = map[string]usageModelResponse{}
			}
			for model, modelRow := range day.Models {
				combined := row.Models[model]
				combined.Tokens.add(modelRow.Tokens)
				combined.CostUSD += modelRow.CostUSD
				combined.PriceSet = combined.PriceSet || modelRow.PriceSet
				row.Models[model] = combined
			}
			byDay[day.Day] = row
		}
		current.Days = current.Days[:0]
		for _, day := range byDay {
			current.Days = append(current.Days, day)
		}
		sortUsageDays(current.Days)
		current.Sessions += incoming.Sessions
		if current.Quota == nil {
			current.Quota = incoming.Quota
		}
		all.Providers[provider] = current
	}
}

func sortUsageDays(days []usageDayResponse) {
	for i := 1; i < len(days); i++ {
		for j := i; j > 0 && days[j].Day < days[j-1].Day; j-- {
			days[j], days[j-1] = days[j-1], days[j]
		}
	}
}

func fetchRemoteUsage(parent context.Context, cfg boxConfig, days int) (usageResponse, error) {
	ctx, cancel := context.WithTimeout(parent, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.URL, "/")+"/api/usage?days="+strconv.Itoa(days), nil)
	if err != nil {
		return usageResponse{}, err
	}
	if cfg.Token != "" {
		request.Header.Set("Authorization", "Bearer "+cfg.Token)
	}
	response, err := (&http.Client{}).Do(request)
	if err != nil {
		return usageResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return usageResponse{}, fmt.Errorf("remote returned HTTP %d", response.StatusCode)
	}
	var usage usageResponse
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&usage); err != nil {
		return usageResponse{}, err
	}
	return usage, nil
}
