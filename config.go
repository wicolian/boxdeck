package main

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

type portNumber int

func (p *portNumber) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if strings.HasPrefix(s, `"`) {
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 || n > 65535 {
		return fmt.Errorf("port must be an integer from 0 to 65535")
	}
	*p = portNumber(n)
	return nil
}

type shortcut struct {
	Name string
	Port portNumber
}

func (s *shortcut) UnmarshalJSON(b []byte) error {
	var pair []json.RawMessage
	if err := json.Unmarshal(b, &pair); err != nil {
		return err
	}
	if len(pair) != 2 {
		return fmt.Errorf("quick entries must be [name, port]")
	}
	if err := json.Unmarshal(pair[0], &s.Name); err != nil {
		return err
	}
	return json.Unmarshal(pair[1], &s.Port)
}
func (s shortcut) MarshalJSON() ([]byte, error) { return json.Marshal([2]any{s.Name, s.Port}) }

type config struct {
	Port               portNumber        `json:"port"`
	Bind               string            `json:"bind"`
	Host               string            `json:"host"`
	User               string            `json:"user"`
	Password           string            `json:"password"`
	FilesPort          portNumber        `json:"filesPort"`
	FilesRoot          string            `json:"filesRoot"`
	FilesAuth          bool              `json:"filesAuth"` // Accepted for migration; the unified origin always authenticates.
	TTYDPort           portNumber        `json:"ttydPort"`
	Terminal           bool              `json:"terminal"`
	Known              map[string]string `json:"known"`
	Hide               []portNumber      `json:"hide"`
	Quick              []shortcut        `json:"quick"`
	ReportRoots        []string          `json:"reportRoots"`
	RepoRoots          []string          `json:"repoRoots"`
	ReportDays         int               `json:"reportDays"`
	AgentPattern       string            `json:"agentPattern"`
	Title              string            `json:"title"`
	Mirror             any               `json:"mirror"`
	MirrorBind         string            `json:"mirrorBind"`
	Tokens             []string          `json:"tokens"`
	TokenLabels        map[string]string `json:"tokenLabels,omitempty"`
	FleetToken         string            `json:"fleetToken"`
	Boxes              []boxConfig       `json:"boxes"`
	AllowRun           bool              `json:"allowRun"`
	Pricing            pricingConfig     `json:"pricing"`
	Apps               []appRecipe       `json:"apps"`
	Apns               apnsConfig        `json:"apns,omitempty"`
	Alerts             alertConfig       `json:"alerts"`
	AlertRules         alertRules        `json:"alertRules"`
	AlertRulesComplete bool              `json:"alertRulesComplete"`
	Probes             []alertProbe      `json:"probes"`
	Quiet              alertQuiet        `json:"quiet"`
	QuietAllowCritical bool              `json:"quietAllowCritical"`
	Sinks              []alertSink       `json:"sinks"`
	WatchPIDs          []int             `json:"watchPids"`
	WatchApps          []string          `json:"watchApps"`
	home, path         string
	agentRE            *regexp.Regexp
}

func configPath(home string) string {
	if p := os.Getenv("BOXDECK_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(home, ".config", "boxdeck", "config.json")
}
func expandHome(p, home string) string {
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
func normalizeHost(host string) (string, error) {
	host = strings.TrimSpace(host)
	if !strings.Contains(host, "://") {
		host = "http://" + host
	}
	u, err := url.Parse(host)
	if err != nil || u.Hostname() == "" || u.User != nil || (u.Scheme != "http" && u.Scheme != "https") || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("host must be a hostname or an http(s) URL without a path")
	}
	return u.Hostname(), nil
}
func loadConfig(path, home string) (config, error) {
	host, _ := os.Hostname()
	_, ttyErr := exec.LookPath("ttyd")
	c := config{Port: 8100, Bind: "127.0.0.1", Host: host, User: "admin", FilesRoot: home, FilesAuth: true, TTYDPort: 7681, Terminal: ttyErr == nil, ReportDays: 3, Title: "Deck", Mirror: "auto", ReportRoots: []string{"~/reports", "~/box"}, RepoRoots: []string{"~", "~/codes", "~/src", "~/projects"}, Hide: []portNumber{22, 53, 111, 139, 445}, Quick: []shortcut{}, Known: map[string]string{"3000": "App", "3001": "App", "4000": "API", "5173": "Vite", "4173": "Vite preview", "8080": "HTTP", "8000": "HTTP", "6006": "Storybook", "5432": "Postgres", "6379": "Redis", "27017": "MongoDB", "9222": "Chrome CDP", "7681": "Terminal (ttyd)", "8384": "Syncthing", "3773": "T3 Code"}, Tokens: []string{}, Boxes: []boxConfig{}, AgentPattern: `^(\S*/)?(claude|codex|aider|opencode|goose)(\s|$)`, home: home, path: path}
	b, err := os.ReadFile(path)
	if err == nil {
		if err = json.Unmarshal(b, &c); err != nil {
			return c, fmt.Errorf("read config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return c, err
	}
	for key, p := range map[string]*string{"BIND": &c.Bind, "HOST": &c.Host, "USER": &c.User, "PASSWORD": &c.Password, "FILES_ROOT": &c.FilesRoot, "MIRROR_BIND": &c.MirrorBind, "FLEET_TOKEN": &c.FleetToken} {
		if v, ok := os.LookupEnv("BOXDECK_" + key); ok {
			*p = v
		}
	}
	for key, p := range map[string]*portNumber{"PORT": &c.Port, "FILES_PORT": &c.FilesPort, "TTYD_PORT": &c.TTYDPort} {
		if v, ok := os.LookupEnv("BOXDECK_" + key); ok {
			b, _ := json.Marshal(v)
			if err = p.UnmarshalJSON(b); err != nil {
				return c, fmt.Errorf("BOXDECK_%s: %w", key, err)
			}
		}
	}
	for key, p := range map[string]*bool{"TERMINAL": &c.Terminal, "FILES_AUTH": &c.FilesAuth} {
		if v, ok := os.LookupEnv("BOXDECK_" + key); ok {
			*p = v != "0" && v != "false"
		}
	}
	if v, ok := os.LookupEnv("BOXDECK_MIRROR"); ok {
		if v == "auto" {
			c.Mirror = "auto"
		} else {
			c.Mirror = v != "0" && v != "false"
		}
	}
	if c.Host, err = normalizeHost(c.Host); err != nil {
		return c, err
	}
	if c.Password == "" {
		return c, fmt.Errorf("set a password in %s or BOXDECK_PASSWORD; run boxdeck install to set up", path)
	}
	if c.User == "" || strings.ContainsAny(c.User, ":\r\n") {
		return c, fmt.Errorf("user must be nonempty without colons or newlines")
	}
	if c.Port == 0 || c.TTYDPort == 0 || c.Port == c.TTYDPort || (c.FilesPort != 0 && (c.FilesPort == c.Port || c.FilesPort == c.TTYDPort)) {
		return c, fmt.Errorf("deck, files and ttyd ports must be distinct; only filesPort may be 0")
	}
	if c.ReportDays < 1 {
		return c, fmt.Errorf("reportDays must be positive")
	}
	switch m := c.Mirror.(type) {
	case bool:
	case string:
		if m != "auto" {
			return c, fmt.Errorf("mirror must be true, false or auto")
		}
	default:
		return c, fmt.Errorf("mirror must be true, false or auto")
	}
	c.FilesRoot = expandHome(c.FilesRoot, home)
	c.FilesRoot, err = filepath.Abs(c.FilesRoot)
	if err != nil {
		return c, err
	}
	for i, p := range c.ReportRoots {
		c.ReportRoots[i] = expandHome(p, home)
	}
	for i, p := range c.RepoRoots {
		c.RepoRoots[i] = expandHome(p, home)
	}
	c.Apns.KeyPath = expandHome(c.Apns.KeyPath, home)
	if c.Apns.Environment == "" {
		c.Apns.Environment = "sandbox"
	}
	if c.Apns.Environment != "sandbox" && c.Apns.Environment != "production" {
		return c, fmt.Errorf("apns environment must be sandbox or production")
	}
	c.agentRE, err = regexp.Compile(c.AgentPattern)
	if err != nil {
		return c, fmt.Errorf("agentPattern: %w", err)
	}
	if c.Known == nil {
		c.Known = map[string]string{}
	}
	if c.Tokens == nil {
		c.Tokens = []string{}
	}
	if c.Boxes == nil {
		c.Boxes = []boxConfig{}
	}
	for i := range c.Boxes {
		c.Boxes[i].Name = strings.TrimSpace(c.Boxes[i].Name)
		if c.Boxes[i].Name == "" {
			return c, fmt.Errorf("boxes[%d].name must be nonempty", i)
		}
		c.Boxes[i].URL, err = normalizeBoxURL(c.Boxes[i].URL)
		if err != nil {
			return c, fmt.Errorf("boxes[%d].url: %w", i, err)
		}
	}
	if c.Known[strconv.Itoa(int(c.Port))] == "" {
		c.Known[strconv.Itoa(int(c.Port))] = c.Title + " (this page)"
	}
	if c.FilesPort > 0 && c.Known[strconv.Itoa(int(c.FilesPort))] == "" {
		c.Known[strconv.Itoa(int(c.FilesPort))] = "Files"
	}
	return c, nil
}
func (c config) address(port portNumber) string {
	return net.JoinHostPort(c.Bind, strconv.Itoa(int(port)))
}
func (c config) portURL(port portNumber) string {
	if port == c.FilesPort && port != 0 {
		return "/files/"
	}
	if port == c.TTYDPort {
		return "/term/"
	}
	return "http://" + net.JoinHostPort(c.Host, strconv.Itoa(int(port)))
}
