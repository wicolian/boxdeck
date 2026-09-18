package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// macOS has no /proc, so the collectors that read it on Linux use ps, lsof, sysctl, vm_stat and
// netstat here. Every parser in this file takes the command text as input so it can be tested
// with recorded output. Callers keep the same shapes as the Linux path; the deck, the menu bar
// and the phone apps do not know which one filled them.

// darwinProcess is one row of `ps -axo pid=,ppid=,etime=,pcpu=,rss=,user=,lstart=,args=`.
type darwinProcess struct {
	PID, PPID int
	Secs      int
	CPU       float64
	RSS       int64
	User      string
	Start     uint64 // unix seconds from lstart, used as the kill race guard
	Args      string
}

const darwinPSFormat = "pid=,ppid=,etime=,pcpu=,rss=,user=,lstart=,args="

// parseEtime turns ps elapsed time ([[dd-]hh:]mm:ss) into seconds.
func parseEtime(value string) int {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	days := 0
	if i := strings.Index(value, "-"); i >= 0 {
		days = integer(value[:i])
		value = value[i+1:]
	}
	parts := strings.Split(value, ":")
	seconds := 0
	for _, part := range parts {
		seconds = seconds*60 + integer(part)
	}
	return days*86400 + seconds
}

func parseLstart(value string) uint64 {
	t, err := time.ParseInLocation("Mon Jan _2 15:04:05 2006", strings.Join(strings.Fields(value), " "), time.Local)
	if err != nil {
		return 0
	}
	return uint64(t.Unix())
}

// parseDarwinPS reads darwinPSFormat rows. The first six columns are single words, lstart is
// always five words ("Fri Sep 18 12:34:59 2026"), and the command line is the rest.
func parseDarwinPS(out string) []darwinProcess {
	list := []darwinProcess{}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 11 {
			continue
		}
		pid, ppid := integer(fields[0]), integer(fields[1])
		if pid <= 0 {
			continue
		}
		list = append(list, darwinProcess{
			PID: pid, PPID: ppid, Secs: parseEtime(fields[2]), CPU: number(fields[3]), RSS: int64(number(fields[4]) * 1024),
			User: fields[5], Start: parseLstart(strings.Join(fields[6:11], " ")), Args: strings.Join(fields[11:], " "),
		})
	}
	return list
}

func (p darwinProcess) process() process {
	return process{PID: p.PID, PPID: p.PPID, Secs: p.Secs, CPU: p.CPU, RSS: p.RSS, Args: p.Args}
}

func darwinProcesses(ctx context.Context) []darwinProcess {
	return parseDarwinPS(sh(ctx, "ps", "-axo", darwinPSFormat))
}

// parseLsofListeners reads `lsof -nP -iTCP -sTCP:LISTEN -F pcn`. Each process starts with a p
// line, its command follows on a c line, and every listening socket is an n line such as
// "*:8100" or "127.0.0.1:8100" or "[::1]:8100".
func parseLsofListeners(out string, hide []portNumber) []portInfo {
	hidden := map[int]bool{}
	for _, p := range hide {
		hidden[int(p)] = true
	}
	seen := map[int]portInfo{}
	pid, proc := 0, ""
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, proc = integer(line[1:]), ""
		case 'c':
			proc = line[1:]
		case 'n':
			name := line[1:]
			i := strings.LastIndex(name, ":")
			if i < 0 {
				continue
			}
			port, addr := integer(name[i+1:]), name[:i]
			if port <= 0 || port > 65535 || hidden[port] || isTailnetIP(strings.Trim(addr, "[]")) {
				continue
			}
			p := portInfo{Port: port, Addr: addr, Proc: proc, PID: pid}
			if existing, ok := seen[port]; ok {
				// Prefer the wildcard or IPv4 listener and keep the first process seen.
				if existing.Addr == "*" || strings.HasPrefix(existing.Addr, "127.") || existing.Addr == "0.0.0.0" {
					continue
				}
				if addr != "*" && !strings.HasPrefix(addr, "127.") && addr != "0.0.0.0" {
					continue
				}
				if existing.PID > 0 {
					p.PID, p.Proc = existing.PID, existing.Proc
				}
			}
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

// parseLsofCWDs reads `lsof -a -d cwd -Fpn -p PID,PID`.
func parseLsofCWDs(out string) map[int]string {
	result := map[int]string{}
	pid := 0
	for _, line := range strings.Split(out, "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid = integer(line[1:])
		case 'n':
			if pid > 0 {
				result[pid] = line[1:]
			}
		}
	}
	return result
}

func darwinCWDs(ctx context.Context, pids []int) map[int]string {
	if len(pids) == 0 {
		return map[int]string{}
	}
	ids := make([]string, 0, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			ids = append(ids, strconv.Itoa(pid))
		}
	}
	if len(ids) == 0 {
		return map[int]string{}
	}
	return parseLsofCWDs(shLenient(ctx, "lsof", "-a", "-d", "cwd", "-Fpn", "-p", strings.Join(ids, ",")))
}

// darwinCWDCache answers single PID working directory lookups without a lsof call per poll.
var darwinCWDCache = struct {
	mu   sync.Mutex
	at   time.Time
	cwds map[int]string
}{cwds: map[int]string{}}

func darwinCWD(pid int) string {
	darwinCWDCache.mu.Lock()
	cwd, ok := darwinCWDCache.cwds[pid]
	fresh := time.Since(darwinCWDCache.at) < 10*time.Second
	darwinCWDCache.mu.Unlock()
	if ok && fresh {
		return cwd
	}
	found := darwinCWDs(context.Background(), []int{pid})
	darwinCWDCache.mu.Lock()
	if !fresh {
		darwinCWDCache.cwds = map[int]string{}
		darwinCWDCache.at = time.Now()
	}
	darwinCWDCache.cwds[pid] = found[pid]
	darwinCWDCache.mu.Unlock()
	return found[pid]
}

// processCWD is the working directory of a process on Linux or macOS, or "" when unknown.
func processCWD(pid int) string {
	if pid <= 0 {
		return ""
	}
	if runtime.GOOS == "darwin" {
		return darwinCWD(pid)
	}
	cwd, _ := os.Readlink(fmt.Sprintf("/proc/%d/cwd", pid))
	return cwd
}

// parseEnvironmentValue finds KEY=value in `ps -p PID -wwE -o command=` output, where the
// environment follows the command line as space separated words.
func parseEnvironmentValue(out, key string) string {
	for _, word := range strings.Fields(out) {
		if strings.HasPrefix(word, key+"=") {
			return strings.TrimPrefix(word, key+"=")
		}
	}
	return ""
}

func darwinEnvironmentValue(pid int, key string) string {
	if pid <= 0 {
		return ""
	}
	return parseEnvironmentValue(sh(context.Background(), "ps", "-p", strconv.Itoa(pid), "-wwE", "-o", "command="), key)
}

// parseSwapUsage reads `sysctl -n vm.swapusage`:
// "total = 13312.00M  used = 12788.88M  free = 523.12M  (encrypted)".
func parseSwapUsage(out string) (total, used, free float64) {
	values := map[string]float64{}
	fields := strings.Fields(out)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i+1] == "=" {
			values[fields[i]] = parseSizeSuffix(fields[i+2])
		}
	}
	return values["total"], values["used"], values["free"]
}

func parseSizeSuffix(value string) float64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	multiplier := float64(1)
	switch value[len(value)-1] {
	case 'K':
		multiplier = 1 << 10
	case 'M':
		multiplier = 1 << 20
	case 'G':
		multiplier = 1 << 30
	case 'T':
		multiplier = 1 << 40
	}
	if multiplier != 1 {
		value = value[:len(value)-1]
	}
	return number(value) * multiplier
}

// parseBootTime reads `sysctl -n kern.boottime`: "{ sec = 1789715099, usec = 479183 } Fri ...".
func parseBootTime(out string) int64 {
	fields := strings.Fields(out)
	for i := 0; i+2 < len(fields); i++ {
		if fields[i] == "sec" && fields[i+1] == "=" {
			return int64(number(strings.TrimSuffix(fields[i+2], ",")))
		}
	}
	return 0
}

// parseVMStat reads `vm_stat` and returns the page size and the free plus inactive bytes.
func parseVMStat(out string) (pageSize, available float64) {
	pageSize = 4096
	if i := strings.Index(out, "page size of "); i >= 0 {
		v := strings.Fields(out[i+13:])
		if len(v) > 0 {
			pageSize = number(v[0])
		}
	}
	for _, l := range strings.Split(out, "\n") {
		if strings.HasPrefix(l, "Pages free:") || strings.HasPrefix(l, "Pages inactive:") || strings.HasPrefix(l, "Pages speculative:") {
			p := strings.SplitN(l, ":", 2)
			available += number(strings.Trim(strings.TrimSpace(p[1]), ".")) * pageSize
		}
	}
	return pageSize, available
}

// parseNetstatIB sums the link level rows of `netstat -ibn`, skipping loopback. Columns are
// Name Mtu Network Address Ipkts Ierrs Ibytes Opkts Oerrs Obytes Coll.
func parseNetstatIB(out string) map[string]ioCounter {
	result := map[string]ioCounter{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || !strings.HasPrefix(f[2], "<Link#") || strings.HasPrefix(f[0], "lo") {
			continue
		}
		// With a link address there are 11 columns; without one (utun, gif) there are 10.
		in, outBytes := f[6], f[9]
		if len(f) < 11 {
			in, outBytes = f[5], f[8]
		}
		result[f[0]] = ioCounter{Read: uintNumber(in), Write: uintNumber(outBytes)}
	}
	return result
}

// darwinCPUPercent estimates whole machine CPU use from the per process decaying averages that
// ps reports, capped at 100.
func darwinCPUPercent(processes []darwinProcess, cores int) float64 {
	total := float64(0)
	for _, p := range processes {
		total += p.CPU
	}
	if cores <= 0 {
		cores = 1
	}
	pct := total / float64(cores)
	if pct > 100 {
		pct = 100
	}
	return float64(int(pct + .5))
}

// darwinProcessStart returns the unix start time of one process, for the kill race guard.
func darwinProcessStart(pid int) uint64 {
	return parseLstart(sh(context.Background(), "ps", "-p", strconv.Itoa(pid), "-o", "lstart="))
}

// darwinProcessRSS returns resident bytes for one process.
func darwinProcessRSS(pid int) int64 {
	return int64(number(strings.TrimSpace(sh(context.Background(), "ps", "-p", strconv.Itoa(pid), "-o", "rss="))) * 1024)
}
