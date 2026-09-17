package barclient

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type HTTPError struct {
	Status int
}

func (e *HTTPError) Error() string { return fmt.Sprintf("boxdeck returned HTTP %d", e.Status) }

func DecodeState(data []byte) (State, error) {
	var value State
	err := json.Unmarshal(data, &value)
	if value.Agents == nil {
		value.Agents = []Agent{}
	}
	if value.Ports == nil {
		value.Ports = []Port{}
	}
	return value, err
}

func DecodeUsage(data []byte) (UsageResponse, error) {
	var value UsageResponse
	err := json.Unmarshal(data, &value)
	if value.Providers == nil {
		value.Providers = map[string]UsageProvider{}
	}
	return value, err
}

func DecodeUsageAll(data []byte) (UsageAllResponse, error) {
	var raw struct {
		Providers map[string]UsageProvider `json:"providers"`
		Boxes     []struct {
			Name       string                   `json:"name"`
			URL        string                   `json:"url"`
			Local      bool                     `json:"local"`
			OK         bool                     `json:"ok"`
			Discovered bool                     `json:"discovered"`
			Tag        string                   `json:"tag"`
			Error      string                   `json:"error"`
			Providers  map[string]UsageProvider `json:"providers"`
		} `json:"boxes"`
		UpdatedAt string `json:"updatedAt"`
	}
	err := json.Unmarshal(data, &raw)
	value := UsageAllResponse{Providers: raw.Providers, UpdatedAt: raw.UpdatedAt}
	for _, box := range raw.Boxes {
		value.Boxes = append(value.Boxes, BoxSnapshot{Name: box.Name, URL: box.URL, Local: box.Local, OK: box.OK, Discovered: box.Discovered, Tag: box.Tag, Usage: UsageResponse{Providers: box.Providers}})
	}
	if value.Providers == nil {
		value.Providers = map[string]UsageProvider{}
	}
	if value.Boxes == nil {
		value.Boxes = []BoxSnapshot{}
	}
	return value, err
}

func DecodePeers(data []byte) ([]Peer, error) {
	var value []Peer
	err := json.Unmarshal(data, &value)
	if value == nil {
		value = []Peer{}
	}
	return value, err
}

func DecodeAlerts(data []byte) ([]Alert, error) {
	var value []Alert
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if value == nil {
		value = []Alert{}
	}
	return value, nil
}

type boxCardJSON struct {
	Name       string `json:"name"`
	URL        string `json:"url"`
	Local      bool   `json:"local"`
	OK         bool   `json:"ok"`
	Since      string `json:"since"`
	Discovered bool   `json:"discovered"`
	Tag        string `json:"tag"`
	Health     Health `json:"health"`
	Agents     int    `json:"agents"`
	Ports      int    `json:"ports"`
	Usage      struct {
		Today map[string]UsageSummary `json:"today"`
		Quota map[string]*Quota       `json:"quota"`
	} `json:"usage"`
}

func DecodeBoxes(data []byte) ([]BoxSnapshot, error) {
	var raw []boxCardJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	result := make([]BoxSnapshot, 0, len(raw))
	for _, card := range raw {
		providers := map[string]UsageProvider{}
		for provider, today := range card.Usage.Today {
			providers[provider] = UsageProvider{Today: today, Quota: card.Usage.Quota[provider]}
		}
		for provider, quota := range card.Usage.Quota {
			if _, exists := providers[provider]; !exists {
				providers[provider] = UsageProvider{Quota: quota}
			}
		}
		result = append(result, BoxSnapshot{
			Name: card.Name, URL: card.URL, Local: card.Local, OK: card.OK, Since: card.Since,
			Discovered: card.Discovered, Tag: card.Tag, Health: card.Health, AgentCount: card.Agents,
			PortCount: card.Ports, Usage: UsageResponse{Providers: providers}, UsageByName: providers,
		})
	}
	return result, nil
}

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func NewClient(baseURL, token string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Token: token, HTTP: &http.Client{Timeout: 5 * time.Second}}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 5 * time.Second}
	}
	response, err := httpClient.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, &HTTPError{Status: response.StatusCode}
	}
	return io.ReadAll(io.LimitReader(response.Body, 16<<20))
}

func (c *Client) FetchBox(ctx context.Context, name string) (BoxSnapshot, error) {
	box := BoxSnapshot{Name: name, URL: c.BaseURL, Token: c.Token}
	stateData, err := c.get(ctx, "/api/state")
	if err != nil {
		return box, err
	}
	state, err := DecodeState(stateData)
	if err != nil {
		return box, err
	}
	box.OK, box.State, box.Health = true, state, state.Health
	box.AgentCount, box.PortCount = len(state.Agents), len(state.Ports)
	usageData, usageErr := c.get(ctx, "/api/usage?days=30")
	if usageErr == nil {
		if usage, decodeErr := DecodeUsage(usageData); decodeErr == nil {
			box.Usage = usage
		}
	}
	if alertsData, alertsErr := c.get(ctx, "/api/alerts?state=open"); alertsErr == nil {
		if alerts, decodeErr := DecodeAlerts(alertsData); decodeErr == nil {
			box.Alerts = alerts
		}
	}
	return box, nil
}

func (c *Client) FetchBoxes(ctx context.Context) ([]BoxSnapshot, error) {
	data, err := c.get(ctx, "/api/boxes")
	if err != nil {
		return nil, err
	}
	return DecodeBoxes(data)
}

func (c *Client) FetchUsageAll(ctx context.Context) (UsageAllResponse, error) {
	data, err := c.get(ctx, "/api/usage/all?days=30")
	if err != nil {
		return UsageAllResponse{}, err
	}
	return DecodeUsageAll(data)
}

func (c *Client) FetchPeers(ctx context.Context) ([]Peer, error) {
	data, err := c.get(ctx, "/api/net/peers")
	if err != nil {
		var statusErr *HTTPError
		if asHTTPError(err, &statusErr) && statusErr.Status == http.StatusNotFound {
			return []Peer{}, nil
		}
		return nil, err
	}
	return DecodePeers(data)
}

func asHTTPError(err error, target **HTTPError) bool {
	if err == nil {
		return false
	}
	value, ok := err.(*HTTPError)
	if ok {
		*target = value
	}
	return ok
}

func FetchFleet(ctx context.Context, cfg Config) []BoxSnapshot {
	result := make([]BoxSnapshot, 0, len(cfg.Boxes))
	for _, item := range cfg.Boxes {
		boxContext, cancel := context.WithTimeout(ctx, 5*time.Second)
		client := NewClient(item.URL, item.Token)
		box, err := client.FetchBox(boxContext, item.Name)
		cancel()
		if err != nil {
			box.Since = time.Now().UTC().Format(time.RFC3339)
		}
		result = append(result, box)
	}
	if cfg.FleetToken == "" || len(cfg.Boxes) == 0 {
		return result
	}

	seed := NewClient(cfg.Boxes[0].URL, cfg.FleetToken)
	discoveryContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	discovered, err := seed.FetchBoxes(discoveryContext)
	cancel()
	if err != nil {
		return result
	}
	usageContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	usageAll, usageErr := seed.FetchUsageAll(usageContext)
	cancel()
	peerContext, cancel := context.WithTimeout(ctx, 5*time.Second)
	peers, peerErr := seed.FetchPeers(peerContext)
	cancel()
	knownDiscovered := map[string]bool{}
	for _, card := range discovered {
		if !card.Discovered || configuredBox(result, card) {
			continue
		}
		if usageErr == nil {
			mergeUsageAll(&card, usageAll)
		}
		card.Tag = "tailnet"
		knownDiscovered[card.URL] = true
		result = append(result, card)
	}
	if peerErr == nil {
		for _, peer := range peers {
			if !peer.Boxdeck || peer.URL == "" || knownDiscovered[peer.URL] || configuredURL(cfg, peer.URL) {
				continue
			}
			boxContext, boxCancel := context.WithTimeout(ctx, 5*time.Second)
			card, fetchErr := NewClient(peer.URL, cfg.FleetToken).FetchBox(boxContext, peer.Name)
			boxCancel()
			card.Discovered, card.Tag = true, "tailnet"
			if fetchErr != nil {
				card.Since = time.Now().UTC().Format(time.RFC3339)
			}
			if usageErr == nil {
				mergeUsageAll(&card, usageAll)
			}
			result = append(result, card)
		}
	}
	return result
}

func configuredURL(config Config, target string) bool {
	for _, box := range config.Boxes {
		if strings.TrimRight(box.URL, "/") == strings.TrimRight(target, "/") {
			return true
		}
	}
	return false
}

func configuredBox(boxes []BoxSnapshot, card BoxSnapshot) bool {
	for _, box := range boxes {
		if box.URL == card.URL || (box.Name != "" && box.Name == card.Name) {
			return true
		}
	}
	return false
}

func mergeUsageAll(card *BoxSnapshot, all UsageAllResponse) {
	for _, box := range all.Boxes {
		if box.URL == card.URL || (box.Name != "" && box.Name == card.Name) {
			card.Usage = UsageResponse{Providers: box.Usage.Providers}
			card.UsageByName = box.Usage.Providers
			return
		}
	}
}
