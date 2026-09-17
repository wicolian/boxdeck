package barclient

import "time"

type Config struct {
	Boxes      []BoxConfig `json:"boxes"`
	FleetToken string      `json:"fleetToken,omitempty"`
	RefreshSec int         `json:"refreshSec"`
	OpenWith   string      `json:"openWith"`
	Notify     bool        `json:"notify"`
}

type BoxConfig struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

type Health struct {
	CPU      float64 `json:"cpu"`
	MemUsed  float64 `json:"memUsed"`
	MemTotal float64 `json:"memTotal"`
	Load1    float64 `json:"load1"`
}

type Agent struct {
	Kind   string `json:"kind"`
	Status string `json:"agent_status"`
}

type Port struct {
	Port int `json:"port"`
}

type State struct {
	Host   string  `json:"host"`
	Health Health  `json:"health"`
	Agents []Agent `json:"agents"`
	Ports  []Port  `json:"ports"`
}

type TokenTotals struct {
	In         int64 `json:"in"`
	CachedIn   int64 `json:"cachedIn"`
	CacheWrite int64 `json:"cacheWrite"`
	Out        int64 `json:"out"`
}

func (t TokenTotals) Total() int64 { return t.In + t.CachedIn + t.CacheWrite + t.Out }

type UsageSummary struct {
	Tokens  TokenTotals `json:"tokens"`
	CostUSD float64     `json:"costUsd"`
}

type QuotaWindow struct {
	Pct      float64 `json:"pct"`
	ResetsAt string  `json:"resetsAt"`
}

type Quota struct {
	FiveHour *QuotaWindow `json:"fiveHour"`
	SevenDay *QuotaWindow `json:"sevenDay"`
}

type UsageModel struct {
	Tokens   TokenTotals `json:"tokens"`
	CostUSD  float64     `json:"costUsd"`
	PriceSet bool        `json:"priceSet"`
}

type UsageDay struct {
	Day     string                `json:"day"`
	Tokens  TokenTotals           `json:"tokens"`
	CostUSD float64               `json:"costUsd"`
	Models  map[string]UsageModel `json:"models"`
}

type UsageProvider struct {
	Quota    *Quota       `json:"quota"`
	Today    UsageSummary `json:"today"`
	LocalDay string       `json:"localDay"`
	Days     []UsageDay   `json:"days"`
	Sessions int          `json:"sessions"`
}

type UsageResponse struct {
	Providers map[string]UsageProvider `json:"providers"`
	Device    string                   `json:"device"`
	UpdatedAt string                   `json:"updatedAt"`
}

type BoxSnapshot struct {
	Name        string                   `json:"name"`
	URL         string                   `json:"url"`
	Local       bool                     `json:"local"`
	OK          bool                     `json:"ok"`
	Since       string                   `json:"since"`
	Discovered  bool                     `json:"discovered"`
	Tag         string                   `json:"tag"`
	Health      Health                   `json:"health"`
	AgentCount  int                      `json:"agents"`
	PortCount   int                      `json:"ports"`
	State       State                    `json:"-"`
	Usage       UsageResponse            `json:"-"`
	UsageByName map[string]UsageProvider `json:"-"`
	Alerts      []Alert                  `json:"-"`
	Token       string                   `json:"-"`
}

type AlertAction struct {
	Label  string         `json:"label"`
	Method string         `json:"method"`
	Path   string         `json:"path"`
	Body   map[string]any `json:"body"`
}

type Alert struct {
	ID       string        `json:"id"`
	Box      string        `json:"box"`
	Rule     string        `json:"rule"`
	Severity string        `json:"severity"`
	Title    string        `json:"title"`
	Body     string        `json:"body"`
	At       string        `json:"at"`
	State    string        `json:"state"`
	Link     string        `json:"link"`
	Actions  []AlertAction `json:"actions"`
}

type UsageAllResponse struct {
	Providers map[string]UsageProvider `json:"providers"`
	Boxes     []BoxSnapshot            `json:"boxes"`
	UpdatedAt string                   `json:"updatedAt"`
}

type Peer struct {
	Name    string `json:"name"`
	URL     string `json:"url"`
	Boxdeck bool   `json:"boxdeck"`
}

type LineAction string

const (
	ActionNone     LineAction = ""
	ActionOpen     LineAction = "open"
	ActionAgents   LineAction = "agents"
	ActionUsage    LineAction = "usage"
	ActionAdd      LineAction = "add"
	ActionRefresh  LineAction = "refresh"
	ActionSettings LineAction = "settings"
	ActionQuit     LineAction = "quit"
	ActionAlertAck LineAction = "alert_ack"
	ActionAlert    LineAction = "alert_action"
)

type MenuLine struct {
	Title  string
	Action LineAction
	URL    string
	Token  string
	Body   string
}

type MenuBox struct {
	Title string
	URL   string
	Lines []MenuLine
}

type LocalUsage struct {
	Title string
}

type MenuAlert struct {
	Title       string
	Box         string
	URL         string
	AckURL      string
	Token       string
	ActionURL   string
	ActionLabel string
	ActionBody  string
}

type IconState string

const (
	IconTemplate  IconState = "template"
	IconHealthy   IconState = "healthy"
	IconAttention IconState = "attention"
	IconRust      IconState = "rust"
)

type MenuModel struct {
	Boxes      []MenuBox
	Needs      []MenuAlert
	LocalUsage *LocalUsage
	AddBox     bool
	IconState  IconState
	Tooltip    string
	Generated  time.Time
}
