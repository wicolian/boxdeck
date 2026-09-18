package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type object = map[string]any

// Every collector has its own lock: a slow optional tool cannot serialize other
// collectors, and concurrent clients share one in-flight command.
type memo[T any] struct {
	mu    sync.Mutex
	at    time.Time
	value T
}

func (m *memo[T]) get(ttl time.Duration, fn func() T) T {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.at.IsZero() || time.Since(m.at) >= ttl {
		m.value = fn()
		m.at = time.Now()
	}
	return m.value
}

type boundedOutput struct {
	bytes.Buffer
	limit int
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	if b.Len()+n > b.limit {
		keep := b.limit - b.Len()
		if keep > 0 {
			b.Buffer.Write(p[:keep])
		}
		return 0, fmt.Errorf("command output too large")
	}
	return b.Buffer.Write(p)
}
func command(ctx context.Context, timeout time.Duration, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out := &boundedOutput{limit: 8 << 20}
	cmd.Stdout = out
	if cmd.Run() != nil {
		return ""
	}
	return out.String()
}
func sh(ctx context.Context, name string, args ...string) string {
	return command(ctx, 4*time.Second, name, args...)
}

// shLenient is sh for tools such as lsof that exit nonzero when one of many requested items
// is not visible, while still printing everything that is.
func shLenient(ctx context.Context, name string, args ...string) string {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = time.Second
	out := &boundedOutput{limit: 8 << 20}
	cmd.Stdout = out
	_ = cmd.Run()
	if ctx.Err() != nil {
		return ""
	}
	return out.String()
}
func number(s string) float64 { n, _ := strconv.ParseFloat(s, 64); return n }
func integer(s string) int    { n, _ := strconv.Atoi(s); return n }
func tilde(p, home string) string {
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

type portInfo struct {
	Port      int    `json:"port"`
	Addr      string `json:"addr"`
	Proc      string `json:"proc"`
	PID       int    `json:"pid"`
	CWD       string `json:"cwd"`
	Label     string `json:"label"`
	Ephemeral bool   `json:"ephemeral"`
	Title     string `json:"title"`
	URL       string `json:"url"`
}

var ssProcRE = regexp.MustCompile(`users:\(\("([^"]+)",pid=(\d+)`)

func parseSS(out string, hide []portNumber) []portInfo {
	hidden := map[int]bool{}
	for _, p := range hide {
		hidden[int(p)] = true
	}
	seen := map[int]portInfo{}
	for _, line := range strings.Split(out, "\n") {
		c := strings.Fields(line)
		if len(c) < 5 {
			continue
		}
		i := strings.LastIndex(c[3], ":")
		if i < 0 {
			continue
		}
		port, addr := integer(c[3][i+1:]), c[3][:i]
		if port <= 0 || port > 65535 || hidden[port] || isTailnetIP(strings.Trim(addr, "[]")) || strings.HasPrefix(addr, "[fd7a:115c:a1e0") {
			continue
		}
		p := portInfo{Port: port, Addr: addr}
		if m := ssProcRE.FindStringSubmatch(line); m != nil {
			p.Proc = m[1]
			p.PID = integer(m[2])
			if p.Proc == "MainThread" {
				p.Proc = "node"
			}
		}
		if prev, ok := seen[port]; !ok || prev.Proc == "" {
			seen[port] = p
		}
	}
	list := make([]portInfo, 0, len(seen))
	for _, p := range seen {
		list = append(list, p)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Port < list[j].Port })
	return list
}

type titleEntry struct {
	title string
	at    time.Time
}
type collectors struct {
	ctx         context.Context
	cfg         config
	ports       memo[[]portInfo]
	processes   memo[[]process]
	panes       memo[[]tmuxPane]
	containers  memo[[]object]
	reports     memo[[]object]
	herdr       memo[herdrState]
	titlesMu    sync.Mutex
	titles      map[int]titleEntry
	dockerRetry time.Time
}

var titleRE = regexp.MustCompile(`(?i)<title[^>]*>([^<]{1,80})`)

func (c *collectors) title(port int) string {
	c.titlesMu.Lock()
	old := c.titles[port]
	if time.Since(old.at) < 2*time.Minute {
		c.titlesMu.Unlock()
		return old.title
	}
	c.titles[port] = titleEntry{old.title, time.Now()}
	c.titlesMu.Unlock()
	go func() {
		client := http.Client{Timeout: 1500 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, _ := http.NewRequestWithContext(c.ctx, "GET", fmt.Sprintf("http://127.0.0.1:%d/", port), nil)
		req.Header.Set("Accept", "text/html")
		res, err := client.Do(req)
		if err != nil {
			return
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(res.Body, 65536))
		title := ""
		if m := titleRE.FindStringSubmatch(string(b)); m != nil {
			title = strings.TrimSpace(m[1])
		} else if res.StatusCode == 401 {
			title = "needs login"
		}
		c.titlesMu.Lock()
		c.titles[port] = titleEntry{title, time.Now()}
		c.titlesMu.Unlock()
	}()
	return old.title
}
func (c *collectors) getPorts() []portInfo {
	return c.ports.get(3*time.Second, func() []portInfo {
		var list []portInfo
		cwds := map[int]string{}
		if runtime.GOOS == "darwin" {
			list = parseLsofListeners(shLenient(c.ctx, "lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pcn"), c.cfg.Hide)
			pids := make([]int, 0, len(list))
			for _, p := range list {
				if p.PID > 0 {
					pids = append(pids, p.PID)
				}
			}
			cwds = darwinCWDs(c.ctx, pids)
		} else {
			list = parseSS(sh(c.ctx, "ss", "-H", "-ltnp"), c.cfg.Hide)
		}
		for i := range list {
			p := &list[i]
			if p.PID > 0 {
				cwd, ok := cwds[p.PID]
				if !ok {
					cwd = processCWD(p.PID)
				}
				p.CWD = tilde(cwd, c.cfg.home)
			}
			p.Label = c.cfg.Known[strconv.Itoa(p.Port)]
			p.Ephemeral = p.Port >= 32768 && p.Label == ""
			if !p.Ephemeral {
				p.Title = c.title(p.Port)
			}
			p.URL = c.cfg.portURL(portNumber(p.Port))
		}
		return list
	})
}

type process struct {
	PID, PPID, Secs int
	CPU             float64
	RSS             int64
	Args            string
}

var psRE = regexp.MustCompile(`^\s*(\d+)\s+(\d+)\s+(\d+)\s+([\d.]+)\s+(\d+)\s+(.*)$`)

func parsePS(out string) []process {
	all := []process{}
	for _, line := range strings.Split(out, "\n") {
		m := psRE.FindStringSubmatch(line)
		if m != nil {
			all = append(all, process{integer(m[1]), integer(m[2]), integer(m[3]), number(m[4]), int64(number(m[5]) * 1024), m[6]})
		}
	}
	return all
}
func (c *collectors) getProcesses() []process {
	return c.processes.get(3*time.Second, func() []process {
		if runtime.GOOS == "darwin" {
			rows := darwinProcesses(c.ctx)
			list := make([]process, 0, len(rows))
			for _, row := range rows {
				list = append(list, row.process())
			}
			return list
		}
		return parsePS(sh(c.ctx, "ps", "-eo", "pid=,ppid=,etimes=,pcpu=,rss=,args="))
	})
}

type tmuxPane struct {
	Session, WName, CWD string
	Command             string
	Win, Pane, PID      int
	Attached            bool
}

func (c *collectors) getPanes() []tmuxPane {
	return c.panes.get(3*time.Second, func() []tmuxPane {
		out := sh(c.ctx, "tmux", "list-panes", "-a", "-F", "#{session_name}\t#{window_index}\t#{window_name}\t#{pane_index}\t#{pane_pid}\t#{pane_current_command}\t#{pane_current_path}\t#{session_attached}")
		list := []tmuxPane{}
		for _, l := range strings.Split(strings.TrimSpace(out), "\n") {
			p := strings.Split(l, "\t")
			if len(p) < 8 {
				continue
			}
			list = append(list, tmuxPane{Session: p[0], WName: p[2], CWD: p[6], Command: p[5], Win: integer(p[1]), Pane: integer(p[3]), PID: integer(p[4]), Attached: p[7] != "0"})
		}
		return list
	})
}
func tmuxSessions(panes []tmuxPane) []object {
	sessions := map[string]object{}
	wins := map[string]map[int]bool{}
	names := []string{}
	for _, p := range panes {
		if wins[p.Session] == nil {
			names = append(names, p.Session)
			wins[p.Session] = map[int]bool{}
			sessions[p.Session] = object{"name": p.Session, "attached": p.Attached}
		}
		wins[p.Session][p.Win] = true
	}
	list := []object{}
	for _, name := range names {
		sessions[name]["windows"] = len(wins[name])
		list = append(list, sessions[name])
	}
	return list
}

var modelRE = regexp.MustCompile(`--model[= ]([\w.:-]+)`)

type agentInfo struct {
	PID         int               `json:"pid"`
	Kind        string            `json:"kind"`
	Model       string            `json:"model"`
	Secs        int               `json:"secs"`
	CPU         float64           `json:"cpu"`
	RSS         int64             `json:"rss"`
	CWD         string            `json:"cwd"`
	Pane        string            `json:"pane"`
	Win         string            `json:"win"`
	PaneID      string            `json:"pane_id,omitempty"`
	Title       string            `json:"terminal_title_stripped,omitempty"`
	Status      string            `json:"agent_status,omitempty"`
	Tokens      map[string]string `json:"tokens,omitempty"`
	WorkspaceID string            `json:"workspace_id,omitempty"`
	TabID       string            `json:"tab_id,omitempty"`
}

func paneFromEnvironment(pid int) string {
	if runtime.GOOS == "darwin" {
		return darwinEnvironmentValue(pid, "HERDR_PANE_ID")
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/environ", pid))
	if err != nil {
		return ""
	}
	// Inspect only this key. Never return or log the rest of the environment.
	for _, entry := range bytes.Split(b, []byte{0}) {
		if bytes.HasPrefix(entry, []byte("HERDR_PANE_ID=")) {
			return string(bytes.TrimPrefix(entry, []byte("HERDR_PANE_ID=")))
		}
	}
	return ""
}
func mergeAgents(all []process, panes []tmuxPane, h herdrState, c config, paneID func(int) string) []agentInfo {
	byPID := map[int]process{}
	paneByPID := map[int]tmuxPane{}
	herdrByPane := map[string]herdrAgent{}
	used := map[string]bool{}
	for _, p := range all {
		byPID[p.PID] = p
	}
	for _, p := range panes {
		paneByPID[p.PID] = p
	}
	for _, a := range h.Agents {
		herdrByPane[a.PaneID] = a
	}
	list := []agentInfo{}
	for _, p := range all {
		match := c.agentRE.FindStringSubmatch(p.Args)
		if match == nil {
			continue
		}
		kind := "agent"
		if len(match) > 2 {
			kind = match[2]
		}
		a := agentInfo{PID: p.PID, Kind: kind, Secs: p.Secs, CPU: p.CPU, RSS: p.RSS}
		if m := modelRE.FindStringSubmatch(p.Args); m != nil {
			a.Model = m[1]
		}
		a.CWD = tilde(processCWD(p.PID), c.home)
		cur := p
		for depth := 0; cur.PID != 0 && depth < 12; depth++ {
			if pane, ok := paneByPID[cur.PID]; ok {
				a.Pane = fmt.Sprintf("%s:%d.%d", pane.Session, pane.Win, pane.Pane)
				a.Win = pane.WName
				break
			}
			cur = byPID[cur.PPID]
		}
		if ha, ok := herdrByPane[paneID(p.PID)]; ok && ha.Agent == kind {
			if used[ha.PaneID] {
				continue
			}
			applyHerdr(&a, ha, c.home)
			used[ha.PaneID] = true
		}
		list = append(list, a)
	}
	for _, ha := range h.Agents {
		if !used[ha.PaneID] {
			a := agentInfo{Kind: ha.Agent}
			applyHerdr(&a, ha, c.home)
			list = append(list, a)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Secs > list[j].Secs })
	return list
}
func applyHerdr(a *agentInfo, h herdrAgent, home string) {
	a.PaneID = h.PaneID
	a.Title = h.Title
	a.Status = h.Status
	a.Tokens = h.Tokens
	a.WorkspaceID = h.WorkspaceID
	a.TabID = h.TabID
	if h.CWD != "" {
		a.CWD = tilde(h.CWD, home)
	}
}

var browserPortRE = regexp.MustCompile(`--remote-debugging-port=(\d+)`)

func browserProcesses(all []process) []object {
	list := []object{}
	for _, p := range all {
		if !(strings.Contains(p.Args, "chrome") || strings.Contains(p.Args, "chromium")) || strings.Contains(p.Args, "--type=") || !(strings.Contains(p.Args, "--headless") || strings.Contains(p.Args, "--remote-debugging")) {
			continue
		}
		port := ""
		if m := browserPortRE.FindStringSubmatch(p.Args); m != nil {
			port = m[1]
		}
		rss := p.RSS
		for _, helper := range all {
			if strings.Contains(helper.Args, "chrom") && strings.Contains(helper.Args, "--type=") && (helper.PPID == p.PID || (port != "" && strings.Contains(helper.Args, "--remote-debugging-port="+port))) {
				rss += helper.RSS
			}
		}
		list = append(list, object{"pid": p.PID, "port": port, "secs": p.Secs, "headless": strings.Contains(p.Args, "--headless"), "rss": rss})
	}
	return list
}
func (c *collectors) getDocker() []object {
	return c.containers.get(10*time.Second, func() []object {
		list := []object{}
		if time.Now().Before(c.dockerRetry) {
			return list
		}
		out := sh(c.ctx, "docker", "ps", "--format", "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}")
		if strings.TrimSpace(out) == "" {
			out = sh(c.ctx, "sg", "docker", "-c", `docker ps --format "{{.Names}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}"`)
			if strings.TrimSpace(out) == "" {
				c.dockerRetry = time.Now().Add(5 * time.Minute)
			}
		}
		for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
			p := strings.Split(line, "\t")
			if len(p) >= 4 {
				list = append(list, object{"name": p[0], "image": p[1], "status": p[2], "ports": p[3]})
			}
		}
		return list
	})
}
func (c *collectors) getReports() []object {
	return c.reports.get(20*time.Second, func() []object {
		roots := []string{}
		for _, p := range c.cfg.ReportRoots {
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				roots = append(roots, p)
			}
		}
		list := []object{}
		if len(roots) == 0 {
			return list
		}
		args := append(roots, "-xdev", "-not", "-path", "*/node_modules/*", "-not", "-path", "*/.git/*", "-type", "f", "(", "-name", "*.md", "-o", "-name", "*.html", "-o", "-name", "*.png", "-o", "-name", "*.pdf", ")", "-mtime", fmt.Sprintf("-%d", c.cfg.ReportDays))
		// BSD find lacks -printf; file stat provides the portable timestamp and size.
		args = append(args, "-print0")
		out := command(c.ctx, 8*time.Second, "find", args...)
		for _, p := range strings.Split(out, "\x00") {
			if p == "" {
				continue
			}
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			rel, err := filepath.Rel(c.cfg.FilesRoot, p)
			href := ""
			if err == nil && rel != ".." && !strings.HasPrefix(rel, "../") {
				href = fileURL(rel)
			}
			list = append(list, object{"t": st.ModTime().UnixMilli(), "size": st.Size(), "rel": tilde(p, c.cfg.home), "url": href})
		}
		sort.Slice(list, func(i, j int) bool { return list[i]["t"].(int64) > list[j]["t"].(int64) })
		if len(list) > 25 {
			list = list[:25]
		}
		return list
	})
}
