package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type tokenTotals struct {
	In         int64 `json:"in"`
	CachedIn   int64 `json:"cachedIn"`
	CacheWrite int64 `json:"cacheWrite"`
	Out        int64 `json:"out"`
}

func (t tokenTotals) plus(other tokenTotals) tokenTotals {
	t.In += other.In
	t.CachedIn += other.CachedIn
	t.CacheWrite += other.CacheWrite
	t.Out += other.Out
	return t
}

func (t *tokenTotals) add(other tokenTotals) {
	t.In += other.In
	t.CachedIn += other.CachedIn
	t.CacheWrite += other.CacheWrite
	t.Out += other.Out
}

type usageEvent struct {
	Model     string      `json:"model"`
	Timestamp time.Time   `json:"timestamp"`
	Tokens    tokenTotals `json:"tokens"`
}

type usageFile struct {
	Provider   string       `json:"provider"`
	Size       int64        `json:"size"`
	Offset     int64        `json:"offset"`
	Events     []usageEvent `json:"events"`
	CodexQuota codexQuota   `json:"codexQuota,omitempty"`
	QuotaAt    time.Time    `json:"quotaAt,omitempty"`
}

type usageModelDay struct {
	Tokens   tokenTotals `json:"tokens"`
	CostUSD  float64     `json:"costUsd"`
	PriceSet bool        `json:"priceSet"`
}

type usageDay struct {
	Provider string                   `json:"provider"`
	Day      string                   `json:"day"`
	Tokens   tokenTotals              `json:"tokens"`
	CostUSD  float64                  `json:"costUsd"`
	Sessions int                      `json:"sessions"`
	Models   map[string]usageModelDay `json:"models"`
}

type quotaWindow struct {
	Pct      float64 `json:"pct"`
	ResetsAt string  `json:"resetsAt"`
}

type codexQuota struct {
	Primary   quotaWindow `json:"primary"`
	Secondary quotaWindow `json:"secondary"`
	Present   bool        `json:"present"`
}

type claudeQuotaState struct {
	FiveHour  quotaWindow  `json:"fiveHour"`
	SevenDay  quotaWindow  `json:"sevenDay"`
	Opus      *quotaWindow `json:"opus"`
	Sonnet    *quotaWindow `json:"sonnet"`
	SampledAt time.Time    `json:"sampledAt"`
	Error     string       `json:"error,omitempty"`
	Present   bool         `json:"present"`
}

type quotaSample struct {
	Provider string    `json:"provider"`
	Window   string    `json:"window"`
	Pct      float64   `json:"pct"`
	ResetsAt string    `json:"resetsAt"`
	At       time.Time `json:"at"`
}

type usageStore struct {
	Version      int                  `json:"version"`
	UpdatedAt    time.Time            `json:"updatedAt"`
	Files        map[string]usageFile `json:"files"`
	Days         map[string]usageDay  `json:"days"`
	CodexQuota   codexQuota           `json:"codexQuota"`
	CodexQuotaAt time.Time            `json:"codexQuotaAt,omitempty"`
	ClaudeQuota  claudeQuotaState     `json:"claudeQuota"`
	QuotaHistory []quotaSample        `json:"quotaHistory"`
}

type claudeQuotaResponse struct {
	FiveHour quotaWindowResponse  `json:"five_hour"`
	SevenDay quotaWindowResponse  `json:"seven_day"`
	Opus     *quotaWindowResponse `json:"seven_day_opus"`
	Sonnet   *quotaWindowResponse `json:"seven_day_sonnet"`
}

type quotaWindowResponse struct {
	Pct      float64
	ResetsAt string
}

func (q *quotaWindowResponse) UnmarshalJSON(b []byte) error {
	var raw struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	q.Pct = raw.Utilization
	q.ResetsAt = raw.ResetsAt
	return nil
}

type usageModelResponse struct {
	Tokens   tokenTotals `json:"tokens"`
	CostUSD  float64     `json:"costUsd"`
	PriceSet bool        `json:"priceSet"`
}

type usageDayResponse struct {
	Day     string                        `json:"day"`
	Tokens  tokenTotals                   `json:"tokens"`
	CostUSD float64                       `json:"costUsd"`
	Models  map[string]usageModelResponse `json:"models"`
}

type usageSummary struct {
	Tokens  tokenTotals `json:"tokens"`
	CostUSD float64     `json:"costUsd"`
}

type usageQuota struct {
	FiveHour  *quotaWindow            `json:"fiveHour"`
	SevenDay  *quotaWindow            `json:"sevenDay"`
	Models    map[string]*quotaWindow `json:"models"`
	SampledAt string                  `json:"sampledAt"`
	Error     string                  `json:"error,omitempty"`
}

type usageProviderResponse struct {
	Quota    *usageQuota        `json:"quota"`
	Today    usageSummary       `json:"today"`
	LocalDay string             `json:"localDay"`
	Days     []usageDayResponse `json:"days"`
	Sessions int                `json:"sessions"`
}

type usageResponse struct {
	Providers map[string]usageProviderResponse `json:"providers"`
	Device    string                           `json:"device"`
	UpdatedAt string                           `json:"updatedAt"`
}

type usageService struct {
	cfg        config
	path       string
	mu         sync.RWMutex
	store      usageStore
	scanning   atomic.Bool
	lastWatch  atomic.Int64
	lastScan   atomic.Int64
	lastClaude time.Time
	client     *http.Client
	now        func() time.Time
	started    atomic.Bool
}

func newUsageService(cfg config) *usageService {
	if cfg.Host != "" {
		configuredDevice = cfg.Host
	}
	path := filepath.Join(cfg.home, ".local", "share", "boxdeck", "usage.json")
	store, err := loadUsageStore(path)
	if err != nil {
		store = emptyUsageStore()
	}
	return &usageService{cfg: cfg, path: path, store: store, client: &http.Client{Timeout: 5 * time.Second}, now: time.Now}
}

func emptyUsageStore() usageStore {
	return usageStore{Version: 1, Files: map[string]usageFile{}, Days: map[string]usageDay{}}
}

func (s *usageService) start(ctx context.Context) {
	s.started.Store(true)
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		s.ensureScan(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				lastWatch := time.Unix(0, s.lastWatch.Load())
				interval := 10 * time.Minute
				if !lastWatch.IsZero() && time.Since(lastWatch) < 2*time.Minute {
					interval = time.Minute
				}
				lastScan := time.Unix(0, s.lastScan.Load())
				if lastScan.IsZero() || time.Since(lastScan) >= interval {
					s.ensureScan(ctx)
				}
			}
		}
	}()
}

func (s *usageService) ensureScan(ctx context.Context) {
	if !s.scanning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer s.scanning.Store(false)
		_ = s.scan(ctx)
	}()
}

func (s *usageService) snapshot(ctx context.Context, days int) usageResponse {
	s.lastWatch.Store(s.now().UnixNano())
	if s.started.Load() {
		s.ensureScan(ctx)
	}
	s.mu.RLock()
	store := cloneUsageStore(s.store)
	s.mu.RUnlock()
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}
	return snapshotFromStore(store, days, s.now())
}

func cloneUsageStore(store usageStore) usageStore {
	b, _ := json.Marshal(store)
	var copy usageStore
	if json.Unmarshal(b, &copy) != nil {
		return emptyUsageStore()
	}
	if copy.Files == nil {
		copy.Files = map[string]usageFile{}
	}
	if copy.Days == nil {
		copy.Days = map[string]usageDay{}
	}
	return copy
}

func (s *usageService) scan(ctx context.Context) error {
	s.mu.RLock()
	store := cloneUsageStore(s.store)
	s.mu.RUnlock()
	now := s.now()
	if err := scanStoreRoots(ctx, &store, filepath.Join(s.cfg.home, ".claude"), codexRoot(s.cfg.home), s.cfg.Pricing, now); err != nil {
		return err
	}
	if now.Sub(s.lastClaude) >= 5*time.Minute {
		refreshClaudeQuota(ctx, &store, filepath.Join(s.cfg.home, ".claude", ".credentials.json"), s.client, now)
		s.lastClaude = now
	}
	store.UpdatedAt = now.UTC()
	pruneStore(&store, now)
	if err := saveUsageStore(s.path, store); err != nil {
		return err
	}
	s.mu.Lock()
	s.store = store
	s.lastScan.Store(now.UnixNano())
	s.mu.Unlock()
	return nil
}

func codexRoot(home string) string {
	if root := os.Getenv("CODEX_HOME"); root != "" {
		return root
	}
	return filepath.Join(home, ".codex")
}

func loadUsageStore(path string) (usageStore, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return emptyUsageStore(), nil
		}
		return usageStore{}, err
	}
	var store usageStore
	if err := json.Unmarshal(b, &store); err != nil {
		return usageStore{}, err
	}
	if store.Files == nil {
		store.Files = map[string]usageFile{}
	}
	if store.Days == nil {
		store.Days = map[string]usageDay{}
	}
	return store, nil
}

func saveUsageStore(path string, store usageStore) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".usage-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func scanUsageRoots(claudeRoot, codexRootPath string, pricing pricingConfig, now time.Time) (usageStore, error) {
	store := emptyUsageStore()
	if err := scanStoreRoots(context.Background(), &store, claudeRoot, codexRootPath, pricing, now); err != nil {
		return usageStore{}, err
	}
	return store, nil
}

func scanStoreRoots(ctx context.Context, store *usageStore, claudeRoot, codexRootPath string, pricing pricingConfig, now time.Time) error {
	if store.Files == nil {
		store.Files = map[string]usageFile{}
	}
	seen := map[string]bool{}
	walk := func(root, provider string) error {
		return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return nil
				}
				return err
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if entry.IsDir() {
				return nil
			}
			if !strings.HasSuffix(strings.ToLower(entry.Name()), ".jsonl") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			key := filepath.Clean(path)
			seen[key] = true
			previous, ok := store.Files[key]
			if ok && previous.Size == info.Size() && previous.Offset == info.Size() {
				return nil
			}
			updated, err := parseUsageFile(key, provider)
			if err != nil {
				return err
			}
			updated.Size = info.Size()
			updated.Offset = info.Size()
			store.Files[key] = updated
			return nil
		})
	}
	if err := walk(claudeRoot, "claude"); err != nil {
		return err
	}
	if err := walk(filepath.Join(codexRootPath, "sessions"), "codex"); err != nil {
		return err
	}
	for path := range store.Files {
		if !seen[path] {
			delete(store.Files, path)
		}
	}
	rebuildDays(store, pricing, now)
	return nil
}

func parseUsageFile(path, provider string) (usageFile, error) {
	file := usageFile{Provider: provider, Events: []usageEvent{}}
	f, err := os.Open(path)
	if err != nil {
		return file, err
	}
	defer f.Close()
	scanner := bufio.NewScanner(io.LimitReader(f, 128<<20))
	scanner.Buffer(make([]byte, 4096), 8<<20)
	if provider == "claude" {
		messages := map[string]usageEvent{}
		anonymous := 0
		for scanner.Scan() {
			var raw map[string]json.RawMessage
			if json.Unmarshal(scanner.Bytes(), &raw) != nil {
				continue
			}
			var typ string
			_ = json.Unmarshal(raw["type"], &typ)
			if typ != "assistant" {
				continue
			}
			var message struct {
				ID        string          `json:"id"`
				Model     string          `json:"model"`
				Usage     json.RawMessage `json:"usage"`
				Timestamp string          `json:"timestamp"`
			}
			if json.Unmarshal(raw["message"], &message) != nil {
				continue
			}
			if message.Usage == nil {
				continue
			}
			var usage struct {
				In         int64 `json:"input_tokens"`
				CachedIn   int64 `json:"cache_read_input_tokens"`
				CacheWrite int64 `json:"cache_creation_input_tokens"`
				Out        int64 `json:"output_tokens"`
			}
			if json.Unmarshal(message.Usage, &usage) != nil {
				continue
			}
			stamp := message.Timestamp
			if stamp == "" {
				_ = json.Unmarshal(raw["timestamp"], &stamp)
			}
			when, err := time.Parse(time.RFC3339Nano, stamp)
			if err != nil {
				continue
			}
			id := message.ID
			if id == "" {
				anonymous++
				id = fmt.Sprintf("anonymous-%d", anonymous)
			}
			messages[id] = usageEvent{Model: cleanModel(message.Model), Timestamp: when, Tokens: tokenTotals{In: usage.In, CachedIn: usage.CachedIn, CacheWrite: usage.CacheWrite, Out: usage.Out}}
		}
		if err := scanner.Err(); err != nil {
			return file, err
		}
		for _, event := range messages {
			file.Events = append(file.Events, event)
		}
	} else {
		model := ""
		tier := ""
		var prevTotal tokenTotals
		turns := map[string]tokenTotals{}
		var latest usageEvent
		var latestAt time.Time
		var latestQuota codexQuota
		for scanner.Scan() {
			var raw struct {
				Timestamp string          `json:"timestamp"`
				Type      string          `json:"type"`
				Payload   json.RawMessage `json:"payload"`
			}
			if json.Unmarshal(scanner.Bytes(), &raw) != nil {
				continue
			}
			var payload struct {
				Model       string `json:"model"`
				ServiceTier string `json:"service_tier"`
				Type        string `json:"type"`
				// Codex records the tier once per thread, in a thread settings event.
				ThreadSettings struct {
					ServiceTier string `json:"service_tier"`
					Model       string `json:"model"`
				} `json:"thread_settings"`
				Info struct {
					Total struct {
						In         int64 `json:"input_tokens"`
						CachedIn   int64 `json:"cached_input_tokens"`
						CacheWrite int64 `json:"cache_write_input_tokens"`
						Out        int64 `json:"output_tokens"`
					} `json:"total_token_usage"`
					Last struct {
						In int64 `json:"input_tokens"`
					} `json:"last_token_usage"`
				} `json:"info"`
				RateLimits map[string]struct {
					UsedPercent float64 `json:"used_percent"`
					ResetsAt    int64   `json:"resets_at"`
				} `json:"rate_limits"`
			}
			if json.Unmarshal(raw.Payload, &payload) != nil {
				continue
			}
			if payload.Model != "" {
				model = cleanModel(payload.Model)
			}
			if payload.ServiceTier != "" && payload.ServiceTier != "default" {
				tier = payload.ServiceTier
			}
			if payload.ThreadSettings.ServiceTier != "" && payload.ThreadSettings.ServiceTier != "default" {
				tier = payload.ThreadSettings.ServiceTier
			}
			if payload.ThreadSettings.Model != "" {
				model = cleanModel(payload.ThreadSettings.Model)
			}
			if raw.Type != "event_msg" || payload.Type != "token_count" {
				continue
			}
			when, err := time.Parse(time.RFC3339Nano, raw.Timestamp)
			if err != nil {
				continue
			}
			if when.After(latestAt) || latestAt.IsZero() {
				latestAt = when
				// Codex input_tokens is the whole prompt: cached reads and cache writes are
				// subsets of it, so the uncached part is what is left after both.
				total := tokenTotals{In: payload.Info.Total.In - payload.Info.Total.CachedIn - payload.Info.Total.CacheWrite, CachedIn: payload.Info.Total.CachedIn, CacheWrite: payload.Info.Total.CacheWrite, Out: payload.Info.Total.Out}
				if total.In < 0 {
					total.In = 0
				}
				// Per turn: the delta since the previous token_count, billed at the long
				// context rate when this turn's input crossed the threshold, and at the
				// session's service tier.
				delta := tokenTotals{In: total.In - prevTotal.In, CachedIn: total.CachedIn - prevTotal.CachedIn, CacheWrite: total.CacheWrite - prevTotal.CacheWrite, Out: total.Out - prevTotal.Out}
				if delta.In < 0 || delta.CachedIn < 0 || delta.CacheWrite < 0 || delta.Out < 0 {
					delta = total // the counter reset (new thread); count the whole thing once
				}
				prevTotal = total
				key := model
				if tier != "" {
					key += "@" + tier
				}
				if payload.Info.Last.In > longContextTokens {
					key += "+long"
				}
				turns[key] = turns[key].plus(delta)
				latest = usageEvent{Model: model, Timestamp: when, Tokens: total}
				for name, window := range payload.RateLimits {
					reset := ""
					if window.ResetsAt > 0 {
						reset = time.Unix(window.ResetsAt, 0).UTC().Format(time.RFC3339)
					}
					parsed := quotaWindow{Pct: window.UsedPercent, ResetsAt: reset}
					switch name {
					case "primary":
						latestQuota.Primary = parsed
					case "secondary":
						latestQuota.Secondary = parsed
					}
				}
				latestQuota.Present = len(payload.RateLimits) > 0
			}
		}
		if err := scanner.Err(); err != nil {
			return file, err
		}
		if !latestAt.IsZero() {
			file.Events = nil
			for key, tokens := range turns {
				file.Events = append(file.Events, usageEvent{Model: key, Timestamp: latest.Timestamp, Tokens: tokens})
			}
			sort.Slice(file.Events, func(i, j int) bool { return file.Events[i].Model < file.Events[j].Model })
			file.CodexQuota = latestQuota
			file.QuotaAt = latestAt
		}
	}
	return file, nil
}

func cleanModel(model string) string {
	model = strings.TrimSpace(model)
	if slash := strings.LastIndex(model, "/"); slash >= 0 {
		model = model[slash+1:]
	}
	return model
}

// usageFile keeps quota metadata out of the persisted event list through a private field.
// It is rebuilt from Codex files on every changed-file parse.
func (f usageFile) quota() codexQuota { return f.CodexQuota }

func rebuildDays(store *usageStore, pricing pricingConfig, now time.Time) {
	store.Days = map[string]usageDay{}
	sessionSeen := map[string]map[string]bool{}
	store.CodexQuota = codexQuota{}
	store.CodexQuotaAt = time.Time{}
	for path, file := range store.Files {
		if file.Provider == "codex" && file.quota().Present && file.QuotaAt.After(now.Add(-24*time.Hour)) && (store.CodexQuotaAt.IsZero() || file.QuotaAt.After(store.CodexQuotaAt)) {
			store.CodexQuota = file.quota()
			store.CodexQuotaAt = file.QuotaAt
		}
		for _, event := range file.Events {
			if event.Tokens.In == 0 && event.Tokens.CachedIn == 0 && event.Tokens.CacheWrite == 0 && event.Tokens.Out == 0 {
				continue // synthetic or empty messages carry no usage and would only add noise rows
			}
			day := event.Timestamp.UTC().Format("2006-01-02")
			key := file.Provider + "|" + day
			row := store.Days[key]
			row.Provider = file.Provider
			row.Day = day
			row.Tokens.add(event.Tokens)
			if row.Models == nil {
				row.Models = map[string]usageModelDay{}
			}
			model := event.Model
			modelRow := row.Models[model]
			modelRow.Tokens.add(event.Tokens)
			p := pricingFor(model, pricing)
			modelRow.CostUSD += estimateCost(event.Tokens, p)
			modelRow.PriceSet = modelRow.PriceSet || p.Set
			row.Models[model] = modelRow
			row.CostUSD += estimateCost(event.Tokens, p)
			if sessionSeen[key] == nil {
				sessionSeen[key] = map[string]bool{}
			}
			if !sessionSeen[key][path] {
				row.Sessions++
				sessionSeen[key][path] = true
			}
			store.Days[key] = row
		}
	}
	if store.CodexQuota.Present {
		store.QuotaHistory = appendQuotaSample(store.QuotaHistory, "codex", "fiveHour", store.CodexQuota.Primary, now)
		store.QuotaHistory = appendQuotaSample(store.QuotaHistory, "codex", "sevenDay", store.CodexQuota.Secondary, now)
	}
	store.UpdatedAt = now.UTC()
}

func pruneStore(store *usageStore, now time.Time) {
	cutoff := now.UTC().AddDate(0, 0, -30).Format("2006-01-02")
	for key, day := range store.Days {
		if day.Day < cutoff {
			delete(store.Days, key)
		}
	}
	cutoffTime := now.AddDate(0, 0, -30)
	filtered := store.QuotaHistory[:0]
	for _, sample := range store.QuotaHistory {
		if sample.At.After(cutoffTime) {
			filtered = append(filtered, sample)
		}
	}
	store.QuotaHistory = filtered
}

func snapshotFromStore(store usageStore, days int, now time.Time) usageResponse {
	if days < 1 {
		days = 1
	}
	if days > 30 {
		days = 30
	}
	providers := map[string]usageProviderResponse{}
	for _, provider := range []string{"claude", "codex"} {
		response := usageProviderResponse{Days: []usageDayResponse{}}
		localDay := now.In(time.Local).Format("2006-01-02")
		response.LocalDay = localDay
		for _, day := range store.Days {
			if day.Provider != provider {
				continue
			}
			if day.Day == localDay {
				response.Today = usageSummary{Tokens: day.Tokens, CostUSD: day.CostUSD}
			}
			if day.Day >= now.UTC().AddDate(0, 0, -(days-1)).Format("2006-01-02") && day.Day <= now.UTC().Format("2006-01-02") {
				models := map[string]usageModelResponse{}
				for model, item := range day.Models {
					models[model] = usageModelResponse{Tokens: item.Tokens, CostUSD: item.CostUSD, PriceSet: item.PriceSet}
				}
				response.Days = append(response.Days, usageDayResponse{Day: day.Day, Tokens: day.Tokens, CostUSD: day.CostUSD, Models: models})
			}
		}
		sort.Slice(response.Days, func(i, j int) bool { return response.Days[i].Day < response.Days[j].Day })
		response.Sessions = countSessions(store.Files, provider, now.UTC().AddDate(0, 0, -(days-1)), now.UTC())
		if provider == "claude" && store.ClaudeQuota.Present {
			response.Quota = &usageQuota{FiveHour: &store.ClaudeQuota.FiveHour, SevenDay: &store.ClaudeQuota.SevenDay, SampledAt: store.ClaudeQuota.SampledAt.UTC().Format(time.RFC3339), Error: store.ClaudeQuota.Error}
			if store.ClaudeQuota.Opus != nil {
				if response.Quota.Models == nil {
					response.Quota.Models = map[string]*quotaWindow{}
				}
				response.Quota.Models["opus"] = store.ClaudeQuota.Opus
			}
			if store.ClaudeQuota.Sonnet != nil {
				if response.Quota.Models == nil {
					response.Quota.Models = map[string]*quotaWindow{}
				}
				response.Quota.Models["sonnet"] = store.ClaudeQuota.Sonnet
			}
		}
		if provider == "codex" && store.CodexQuota.Present {
			response.Quota = &usageQuota{FiveHour: &store.CodexQuota.Primary, SevenDay: &store.CodexQuota.Secondary}
		}
		providers[provider] = response
	}
	updated := store.UpdatedAt
	if updated.IsZero() {
		updated = now
	}
	return usageResponse{Providers: providers, Device: deviceName(), UpdatedAt: updated.UTC().Format(time.RFC3339)}
}

func countSessions(files map[string]usageFile, provider string, since, until time.Time) int {
	count := 0
	for _, file := range files {
		if file.Provider != provider {
			continue
		}
		for _, event := range file.Events {
			if !event.Timestamp.Before(since) && !event.Timestamp.After(until) {
				count++
				break
			}
		}
	}
	return count
}

func refreshClaudeQuota(ctx context.Context, store *usageStore, credentialsPath string, client *http.Client, now time.Time) {
	b, err := os.ReadFile(credentialsPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return
		}
		store.ClaudeQuota.Error = "could not read Claude login"
		return
	}
	var credentials struct {
		Claude struct {
			AccessToken string  `json:"accessToken"`
			ExpiresAt   float64 `json:"expiresAt"`
		} `json:"claudeAiOauth"`
	}
	if json.Unmarshal(b, &credentials) != nil || credentials.Claude.AccessToken == "" {
		store.ClaudeQuota.Error = "Claude login is unavailable"
		return
	}
	if credentials.Claude.ExpiresAt > 0 && now.UnixMilli() >= int64(credentials.Claude.ExpiresAt) {
		store.ClaudeQuota.Error = "open Claude Code once to refresh the login"
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.anthropic.com/api/oauth/usage", nil)
	if err != nil {
		return
	}
	req.Header.Set("Authorization", "Bearer "+credentials.Claude.AccessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")
	response, err := client.Do(req)
	if err != nil {
		store.ClaudeQuota.Error = "Claude quota is temporarily unavailable"
		return
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		store.ClaudeQuota.Error = "Claude quota is temporarily unavailable"
		return
	}
	var payload claudeQuotaResponse
	if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&payload) != nil {
		store.ClaudeQuota.Error = "Claude quota response was invalid"
		return
	}
	store.ClaudeQuota = claudeQuotaState{
		FiveHour:  quotaWindow{Pct: payload.FiveHour.Pct, ResetsAt: payload.FiveHour.ResetsAt},
		SevenDay:  quotaWindow{Pct: payload.SevenDay.Pct, ResetsAt: payload.SevenDay.ResetsAt},
		SampledAt: now.UTC(), Present: true,
	}
	if payload.Opus != nil {
		store.ClaudeQuota.Opus = &quotaWindow{Pct: payload.Opus.Pct, ResetsAt: payload.Opus.ResetsAt}
	}
	if payload.Sonnet != nil {
		store.ClaudeQuota.Sonnet = &quotaWindow{Pct: payload.Sonnet.Pct, ResetsAt: payload.Sonnet.ResetsAt}
	}
	store.QuotaHistory = appendQuotaSample(store.QuotaHistory, "claude", "fiveHour", store.ClaudeQuota.FiveHour, now)
	store.QuotaHistory = appendQuotaSample(store.QuotaHistory, "claude", "sevenDay", store.ClaudeQuota.SevenDay, now)
}

func appendQuotaSample(history []quotaSample, provider, window string, quota quotaWindow, now time.Time) []quotaSample {
	for i := len(history) - 1; i >= 0; i-- {
		if history[i].Provider == provider && history[i].Window == window && now.Sub(history[i].At) < 5*time.Minute {
			return history
		}
	}
	return append(history, quotaSample{Provider: provider, Window: window, Pct: quota.Pct, ResetsAt: quota.ResetsAt, At: now.UTC()})
}

// deviceName is what this machine calls itself in usage output: the configured host name when
// boxdeck runs as a server, else the OS hostname (the standalone "boxdeck usage" case).
var configuredDevice string

func deviceName() string {
	if configuredDevice != "" {
		return configuredDevice
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return "this device"
}
