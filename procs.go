package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

type cpuCounter struct{ Total, Idle uint64 }
type ioCounter struct{ Read, Write uint64 }
type procStat struct {
	PID, PPID    int
	Name         string
	Ticks, Start uint64
	RSS          int64
}
type procInfo struct {
	PID   int      `json:"pid"`
	Name  string   `json:"name"`
	CPU   float64  `json:"cpu"`
	Mem   int64    `json:"mem"`
	Age   float64  `json:"age"`
	User  string   `json:"user"`
	CWD   string   `json:"cwd"`
	Tags  []string `json:"tags"`
	Start uint64   `json:"startTicks"`
	ppid  int
	args  string
}
type liveMetrics struct {
	Time         int64      `json:"time"`
	CPU          float64    `json:"cpu"`
	Cores        []float64  `json:"cores"`
	CoreCount    int        `json:"coreCount"`
	MemTotal     uint64     `json:"memTotal"`
	MemUsed      uint64     `json:"memUsed"`
	SwapTotal    uint64     `json:"swapTotal"`
	SwapUsed     uint64     `json:"swapUsed"`
	Load         []float64  `json:"load"`
	Uptime       float64    `json:"uptime"`
	NetRX        float64    `json:"netRx"`
	NetTX        float64    `json:"netTx"`
	DiskRead     float64    `json:"diskRead"`
	DiskWrite    float64    `json:"diskWrite"`
	TopCPU       []procInfo `json:"topCpu"`
	TopMem       []procInfo `json:"topMem"`
	ProcessCount int        `json:"processCount"`
	Available    bool       `json:"available"`
}
type procMeta struct {
	start           uint64
	at              time.Time
	user, cwd, args string
}
type procSampler struct {
	root, sysRoot, home string
	prevCPU             map[string]cpuCounter
	prevNet, prevDisk   map[string]ioCounter
	prevProc            map[int]procStat
	metadata            map[int]procMeta
	users               map[string]string
	at                  time.Time
	all                 []procInfo
}

func newProcSampler(root, sysRoot, home string) *procSampler {
	users := map[string]string{}
	for _, line := range strings.Split(readFile("/etc/passwd"), "\n") {
		f := strings.Split(line, ":")
		if len(f) > 2 {
			users[f[2]] = f[0]
		}
	}
	return &procSampler{root: root, sysRoot: sysRoot, home: home, users: users, metadata: map[int]procMeta{}}
}
func uintNumber(s string) uint64 { n, _ := strconv.ParseUint(s, 10, 64); return n }
func delta(now, old uint64) uint64 {
	if now < old {
		return 0
	}
	return now - old
}
func parseCPUs(text string) map[string]cpuCounter {
	result := map[string]cpuCounter{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || !strings.HasPrefix(f[0], "cpu") {
			continue
		}
		var c cpuCounter
		// guest and guest_nice are already included in user and nice.
		for _, value := range f[1:min(len(f), 9)] {
			c.Total += uintNumber(value)
		}
		c.Idle = uintNumber(f[4])
		if len(f) > 5 {
			c.Idle += uintNumber(f[5])
		}
		result[f[0]] = c
	}
	return result
}
func cpuUsage(now, prev cpuCounter) float64 {
	total := delta(now.Total, prev.Total)
	if total == 0 {
		return 0
	}
	idle := min(total, delta(now.Idle, prev.Idle))
	return math.Round(1000*float64(total-idle)/float64(total)) / 10
}
func parseNet(text string) map[string]ioCounter {
	out := map[string]ioCounter{}
	for _, line := range strings.Split(text, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		name := strings.TrimSpace(parts[0])
		f := strings.Fields(parts[1])
		if name == "lo" || len(f) < 9 {
			continue
		}
		out[name] = ioCounter{uintNumber(f[0]), uintNumber(f[8])}
	}
	return out
}
func parseDisk(text string, whole func(string) bool) map[string]ioCounter {
	out := map[string]ioCounter{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 14 || !whole(f[2]) {
			continue
		}
		out[f[2]] = ioCounter{uintNumber(f[5]) * 512, uintNumber(f[9]) * 512}
	}
	return out
}
func ioRates(now, prev map[string]ioCounter, seconds float64) (float64, float64) {
	if seconds <= 0 {
		return 0, 0
	}
	var read, write uint64
	for name, c := range now {
		if p, ok := prev[name]; ok {
			read += delta(c.Read, p.Read)
			write += delta(c.Write, p.Write)
		}
	}
	return float64(read) / seconds, float64(write) / seconds
}
func parseProcStat(text string) (procStat, error) {
	left, right := strings.Index(text, "("), strings.LastIndex(text, ")")
	if left < 1 || right < left {
		return procStat{}, fmt.Errorf("malformed process stat")
	}
	f := strings.Fields(text[right+1:])
	if len(f) < 22 {
		return procStat{}, fmt.Errorf("short process stat")
	}
	pid, err := strconv.Atoi(strings.TrimSpace(text[:left]))
	if err != nil {
		return procStat{}, err
	}
	return procStat{PID: pid, Name: text[left+1 : right], PPID: integer(f[1]), Ticks: uintNumber(f[11]) + uintNumber(f[12]), Start: uintNumber(f[19]), RSS: int64(number(f[21]))}, nil
}
func (s *procSampler) wholeDisk(name string) bool {
	// /sys/block contains whole devices only. Skip virtual stacking devices to
	// avoid counting the same I/O again through device-mapper, RAID or loopbacks.
	if strings.HasPrefix(name, "loop") || strings.HasPrefix(name, "ram") || strings.HasPrefix(name, "dm-") || strings.HasPrefix(name, "md") {
		return false
	}
	_, err := os.Stat(filepath.Join(s.sysRoot, "block", name))
	return err == nil
}
func (s *procSampler) sample(now time.Time) liveMetrics {
	m := liveMetrics{Time: now.UnixMilli(), Cores: []float64{}, Load: []float64{0, 0, 0}, TopCPU: []procInfo{}, TopMem: []procInfo{}}
	cpus := parseCPUs(readFile(filepath.Join(s.root, "stat")))
	m.Available = len(cpus) > 0
	if !m.Available && runtime.GOOS == "darwin" {
		return s.sampleDarwin(now)
	}
	if !m.Available {
		s.at = now
		s.all = []procInfo{}
		return m
	}
	seconds := now.Sub(s.at).Seconds()
	if s.at.IsZero() {
		seconds = 0
	}
	if prev, ok := s.prevCPU["cpu"]; ok {
		m.CPU = cpuUsage(cpus["cpu"], prev)
	}
	names := []string{}
	for name := range cpus {
		if name != "cpu" {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return integer(names[i][3:]) < integer(names[j][3:]) })
	for _, name := range names {
		v := float64(0)
		if prev, ok := s.prevCPU[name]; ok {
			v = cpuUsage(cpus[name], prev)
		}
		m.Cores = append(m.Cores, v)
	}
	m.CoreCount = len(m.Cores)
	mem := map[string]uint64{}
	for _, line := range strings.Split(readFile(filepath.Join(s.root, "meminfo")), "\n") {
		f := strings.Fields(line)
		if len(f) > 1 {
			mem[strings.TrimSuffix(f[0], ":")] = uintNumber(f[1]) * 1024
		}
	}
	m.MemTotal = mem["MemTotal"]
	m.MemUsed = delta(m.MemTotal, mem["MemAvailable"])
	m.SwapTotal = mem["SwapTotal"]
	m.SwapUsed = delta(m.SwapTotal, mem["SwapFree"])
	for i, v := range strings.Fields(readFile(filepath.Join(s.root, "loadavg"))) {
		if i >= 3 {
			break
		}
		m.Load[i] = number(v)
	}
	if f := strings.Fields(readFile(filepath.Join(s.root, "uptime"))); len(f) > 0 {
		m.Uptime = number(f[0])
	}
	netNow := parseNet(readFile(filepath.Join(s.root, "net/dev")))
	diskNow := parseDisk(readFile(filepath.Join(s.root, "diskstats")), s.wholeDisk)
	m.NetRX, m.NetTX = ioRates(netNow, s.prevNet, seconds)
	m.DiskRead, m.DiskWrite = ioRates(diskNow, s.prevDisk, seconds)
	processes := map[int]procStat{}
	all := []procInfo{}
	entries, _ := os.ReadDir(s.root)
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil || pid < 1 || !entry.IsDir() {
			continue
		}
		base := filepath.Join(s.root, entry.Name())
		p, err := parseProcStat(readFile(filepath.Join(base, "stat")))
		if err != nil {
			continue
		}
		processes[pid] = p
		rss := p.RSS * int64(os.Getpagesize())
		if f := strings.Fields(readFile(filepath.Join(base, "statm"))); len(f) > 1 {
			rss = int64(uintNumber(f[1])) * int64(os.Getpagesize())
		}
		meta, ok := s.metadata[pid]
		if !ok || meta.start != p.Start || now.Sub(meta.at) > 10*time.Second {
			uid := ""
			for _, line := range strings.Split(readFile(filepath.Join(base, "status")), "\n") {
				if strings.HasPrefix(line, "Uid:") {
					f := strings.Fields(line)
					if len(f) > 1 {
						uid = f[1]
					}
					break
				}
			}
			user := uid
			if name := s.users[uid]; name != "" {
				user = name
			}
			cwd, _ := os.Readlink(filepath.Join(base, "cwd"))
			args := strings.TrimSpace(strings.ReplaceAll(readFile(filepath.Join(base, "cmdline")), "\x00", " "))
			meta = procMeta{p.Start, now, user, tilde(cwd, s.home), args}
			s.metadata[pid] = meta
		}
		cpu := float64(0)
		if prev, ok := s.prevProc[pid]; ok && prev.Start == p.Start && seconds > 0 {
			cpu = 100 * float64(delta(p.Ticks, prev.Ticks)) / (seconds * 100)
		} // Linux USER_HZ is 100 on supported amd64/arm64.
		all = append(all, procInfo{PID: pid, Name: p.Name, CPU: math.Round(cpu*10) / 10, Mem: rss, Age: max(0, m.Uptime-float64(p.Start)/100), User: meta.user, CWD: meta.cwd, Tags: []string{}, Start: p.Start, ppid: p.PPID, args: meta.args})
	}
	for pid := range s.metadata {
		if _, ok := processes[pid]; !ok {
			delete(s.metadata, pid)
		}
	}
	s.all = all
	m.ProcessCount = len(all)
	m.TopCPU = sortedProcs(all, "cpu", 8)
	m.TopMem = sortedProcs(all, "mem", 8)
	s.prevCPU, s.prevNet, s.prevDisk, s.prevProc, s.at = cpus, netNow, diskNow, processes, now
	return m
}

// sampleDarwin fills the same liveMetrics from ps, sysctl, vm_stat and netstat. Per core
// figures are not available without a kernel call, so Cores stays empty and the view shows
// the whole machine number. Working directories are resolved for the top lists only, with
// one lsof call, and cached for ten seconds.
func (s *procSampler) sampleDarwin(now time.Time) liveMetrics {
	ctx := context.Background()
	m := liveMetrics{Time: now.UnixMilli(), Cores: []float64{}, CoreCount: runtime.NumCPU(), Load: []float64{0, 0, 0}, TopCPU: []procInfo{}, TopMem: []procInfo{}, Available: true}
	seconds := now.Sub(s.at).Seconds()
	if s.at.IsZero() {
		seconds = 0
	}
	rows := darwinProcesses(ctx)
	m.CPU = darwinCPUPercent(rows, runtime.NumCPU())
	m.MemTotal = uint64(number(strings.TrimSpace(sh(ctx, "sysctl", "-n", "hw.memsize"))))
	_, available := parseVMStat(sh(ctx, "vm_stat"))
	m.MemUsed = delta(m.MemTotal, uint64(available))
	swapTotal, swapUsed, _ := parseSwapUsage(sh(ctx, "sysctl", "-n", "vm.swapusage"))
	m.SwapTotal, m.SwapUsed = uint64(swapTotal), uint64(swapUsed)
	for i, v := range strings.Fields(strings.Trim(sh(ctx, "sysctl", "-n", "vm.loadavg"), "{} \n")) {
		if i >= 3 {
			break
		}
		m.Load[i] = number(v)
	}
	if boot := parseBootTime(sh(ctx, "sysctl", "-n", "kern.boottime")); boot > 0 {
		m.Uptime = float64(now.Unix() - boot)
	}
	netNow := parseNetstatIB(sh(ctx, "netstat", "-ibn"))
	m.NetRX, m.NetTX = ioRates(netNow, s.prevNet, seconds)
	all := make([]procInfo, 0, len(rows))
	live := map[int]bool{}
	for _, row := range rows {
		live[row.PID] = true
		all = append(all, procInfo{PID: row.PID, Name: darwinProcessName(row.Args), CPU: math.Round(row.CPU*10) / 10, Mem: row.RSS, Age: float64(row.Secs), User: row.User, Tags: []string{}, Start: row.Start, ppid: row.PPID, args: row.Args})
	}
	top := append(sortedProcs(all, "cpu", 8), sortedProcs(all, "mem", 8)...)
	missing := []int{}
	for _, p := range top {
		meta, ok := s.metadata[p.PID]
		if !ok || meta.start != p.Start || now.Sub(meta.at) > 10*time.Second {
			missing = append(missing, p.PID)
		}
	}
	for pid, cwd := range darwinCWDs(ctx, missing) {
		start := uint64(0)
		for _, p := range top {
			if p.PID == pid {
				start = p.Start
			}
		}
		s.metadata[pid] = procMeta{start, now, "", tilde(cwd, s.home), ""}
	}
	for i := range all {
		if meta, ok := s.metadata[all[i].PID]; ok {
			all[i].CWD = meta.cwd
		}
	}
	for pid := range s.metadata {
		if !live[pid] {
			delete(s.metadata, pid)
		}
	}
	s.all = all
	m.ProcessCount = len(all)
	m.TopCPU = sortedProcs(all, "cpu", 8)
	m.TopMem = sortedProcs(all, "mem", 8)
	s.prevNet, s.at = netNow, now
	return m
}

// darwinProcessName is the short name for a command line: the app name for a bundle path such
// as /Applications/Safari.app/Contents/MacOS/Safari, else the executable basename. ps gives no
// reliable comm column, and executable paths can contain spaces, so an absolute path is grown
// one word at a time until it names a real file. Results are cached per command line.
var darwinNameCache = struct {
	mu    sync.Mutex
	names map[string]string
}{names: map[string]string{}}

func darwinProcessName(args string) string {
	args = strings.TrimSpace(args)
	if args == "" {
		return ""
	}
	darwinNameCache.mu.Lock()
	name, ok := darwinNameCache.names[args]
	darwinNameCache.mu.Unlock()
	if ok {
		return name
	}
	name = darwinProcessNameOf(args, func(path string) bool { info, err := os.Stat(path); return err == nil && !info.IsDir() })
	darwinNameCache.mu.Lock()
	if len(darwinNameCache.names) > 4096 {
		darwinNameCache.names = map[string]string{}
	}
	darwinNameCache.names[args] = name
	darwinNameCache.mu.Unlock()
	return name
}

func darwinProcessNameOf(args string, exists func(string) bool) string {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return ""
	}
	executable := fields[0]
	if strings.HasPrefix(executable, "/") {
		for i := 1; i < len(fields) && i < 12 && !exists(executable); i++ {
			if strings.HasPrefix(fields[i], "-") {
				break
			}
			executable += " " + fields[i]
		}
	}
	if i := strings.Index(executable, ".app/"); i >= 0 {
		bundle := executable[:i]
		return bundle[strings.LastIndex(bundle, "/")+1:]
	}
	return executable[strings.LastIndex(executable, "/")+1:]
}

func sortedProcs(all []procInfo, order string, n int) []procInfo {
	out := append([]procInfo{}, all...)
	sort.Slice(out, func(i, j int) bool {
		if order == "mem" {
			if out[i].Mem != out[j].Mem {
				return out[i].Mem > out[j].Mem
			}
		} else if out[i].CPU != out[j].CPU {
			return out[i].CPU > out[j].CPU
		}
		return out[i].PID < out[j].PID
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// UI routes deliberately use /api/ui/ so the independent public API branch can
// add /api/procs without a duplicate route or a shared implementation edit.
func (a *app) uiProcs(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		jsonReply(w, 405, object{"error": "Use GET to read processes"})
		return
	}
	_, all := a.live.current()
	servers := map[int]bool{}
	for _, p := range a.collect.getPorts() {
		if !p.Ephemeral && p.PID > 0 {
			servers[p.PID] = true
		}
	}
	for i := range all {
		p := &all[i]
		if a.cfg.agentRE != nil && a.cfg.agentRE.MatchString(p.args) {
			p.Tags = append(p.Tags, "agent")
		}
		if strings.Contains(p.args, "chrom") && (strings.Contains(p.args, "--headless") || strings.Contains(p.args, "--remote-debugging")) {
			p.Tags = append(p.Tags, "browser")
		}
		if servers[p.PID] {
			p.Tags = append(p.Tags, "server")
		}
	}
	order := r.URL.Query().Get("sort")
	if order != "mem" {
		order = "cpu"
	}
	n := integer(r.URL.Query().Get("n"))
	if n <= 0 || n > 1000 {
		n = 200
	}
	jsonReply(w, 200, sortedProcs(all, order, n))
}
func killGuard(pid, self int) error {
	if pid <= 1 || pid == self {
		return fmt.Errorf("PID 1, this server and process groups cannot be stopped")
	}
	return nil
}
func (a *app) killProc(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		jsonReply(w, 405, object{"error": "Use POST to stop a process"})
		return
	}
	var body struct {
		PID    int    `json:"pid"`
		Signal string `json:"signal"`
		Start  uint64 `json:"startTicks"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		jsonReply(w, 400, object{"error": "Choose a process and try again"})
		return
	}
	if err := killGuard(body.PID, os.Getpid()); err != nil {
		jsonReply(w, 403, object{"error": err.Error()})
		return
	}
	var signal os.Signal
	switch body.Signal {
	case "", "TERM", "SIGTERM":
		signal = syscall.SIGTERM
		body.Signal = "SIGTERM"
	case "KILL", "SIGKILL":
		signal = syscall.SIGKILL
		body.Signal = "SIGKILL"
	default:
		jsonReply(w, 400, object{"error": "Use SIGTERM or SIGKILL"})
		return
	}
	if runtime.GOOS == "windows" {
		jsonReply(w, 501, object{"error": "Process control is available on Linux and macOS"})
		return
	}
	p, err := os.FindProcess(body.PID)
	if err != nil {
		jsonReply(w, 404, object{"error": "Process has exited. Refresh the list"})
		return
	}
	defer p.Release()
	if body.Start != 0 && runtime.GOOS == "darwin" {
		if darwinProcessStart(body.PID) != body.Start {
			jsonReply(w, 409, object{"error": "This process changed or exited. Refresh the list"})
			return
		}
	} else if body.Start != 0 {
		st, err := parseProcStat(readFile(filepath.Join(a.live.sampler.root, strconv.Itoa(body.PID), "stat")))
		if err != nil || st.Start != body.Start {
			jsonReply(w, 409, object{"error": "This process changed or exited. Refresh the list"})
			return
		}
	}
	if err = p.Signal(signal); err != nil {
		jsonReply(w, 409, object{"error": "Could not stop this process. It may have exited or belong to another user"})
		return
	}
	jsonReply(w, 200, object{"pid": body.PID, "signal": body.Signal, "sent": true})
}
