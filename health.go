package main

import (
	"context"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type healthSampler struct {
	mu                  sync.Mutex
	health              object
	hist                map[string][]float64
	prevIdle, prevTotal float64
	disk                memo[object]
	lastPoll            atomic.Int64
	ctx                 context.Context
}

func (s *healthSampler) watching() bool { return time.Now().UnixMilli()-s.lastPoll.Load() < 15000 }
func sampleDelay(watching bool) time.Duration {
	if watching {
		return 3 * time.Second
	}
	return 30 * time.Second
}
func (s *healthSampler) run() {
	s.sample()
	timer := time.NewTimer(sampleDelay(s.watching()))
	defer timer.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-timer.C:
			s.sample()
			timer.Reset(sampleDelay(s.watching()))
		}
	}
}
func readFile(p string) string { b, _ := os.ReadFile(p); return string(b) }
func (s *healthSampler) sample() {
	h := object{"cpu": float64(0), "cores": runtime.NumCPU(), "load1": float64(0), "load5": float64(0), "load15": float64(0), "memTotal": float64(0), "memUsed": float64(0), "memAvail": float64(0), "swapTotal": float64(0), "swapUsed": float64(0), "swapFree": float64(0), "uptime": float64(0)}
	if runtime.GOOS == "linux" {
		cpu := strings.Fields(strings.SplitN(readFile("/proc/stat"), "\n", 2)[0])
		if len(cpu) > 5 {
			total := float64(0)
			for _, v := range cpu[1:] {
				total += number(v)
			}
			idle := number(cpu[4]) + number(cpu[5])
			if s.prevTotal > 0 && total > s.prevTotal {
				h["cpu"] = float64(int(100*(1-(idle-s.prevIdle)/(total-s.prevTotal)) + .5))
			}
			s.prevIdle, s.prevTotal = idle, total
		}
		m := map[string]float64{}
		for _, l := range strings.Split(readFile("/proc/meminfo"), "\n") {
			v := strings.Fields(l)
			if len(v) > 1 {
				m[strings.TrimSuffix(v[0], ":")] = number(v[1]) * 1024
			}
		}
		h["memTotal"], h["memAvail"], h["memUsed"] = m["MemTotal"], m["MemAvailable"], m["MemTotal"]-m["MemAvailable"]
		h["swapTotal"], h["swapFree"], h["swapUsed"] = m["SwapTotal"], m["SwapFree"], m["SwapTotal"]-m["SwapFree"]
		l := strings.Fields(readFile("/proc/loadavg"))
		if len(l) >= 3 {
			h["load1"], h["load5"], h["load15"] = number(l[0]), number(l[1]), number(l[2])
		}
		u := strings.Fields(readFile("/proc/uptime"))
		if len(u) > 0 {
			h["uptime"] = number(u[0])
		}
	} else {
		h["memTotal"] = number(strings.TrimSpace(sh(s.ctx, "sysctl", "-n", "hw.memsize")))
		loads := strings.Fields(strings.Trim(sh(s.ctx, "sysctl", "-n", "vm.loadavg"), "{} \n"))
		if len(loads) >= 3 {
			h["load1"], h["load5"], h["load15"] = number(loads[0]), number(loads[1]), number(loads[2])
		}
		vm := sh(s.ctx, "vm_stat")
		pageSize := float64(4096)
		if i := strings.Index(vm, "page size of "); i >= 0 {
			v := strings.Fields(vm[i+13:])
			if len(v) > 0 {
				pageSize = number(v[0])
			}
		}
		free := float64(0)
		for _, l := range strings.Split(vm, "\n") {
			if strings.HasPrefix(l, "Pages free:") || strings.HasPrefix(l, "Pages inactive:") {
				p := strings.SplitN(l, ":", 2)
				free += number(strings.Trim(strings.TrimSpace(p[1]), ".")) * pageSize
			}
		}
		h["memAvail"] = free
		h["memUsed"] = max(0, h["memTotal"].(float64)-free)
		swapTotal, swapUsed, swapFree := parseSwapUsage(sh(s.ctx, "sysctl", "-n", "vm.swapusage"))
		h["swapTotal"], h["swapUsed"], h["swapFree"] = swapTotal, swapUsed, swapFree
		if boot := parseBootTime(sh(s.ctx, "sysctl", "-n", "kern.boottime")); boot > 0 {
			h["uptime"] = float64(time.Now().Unix() - boot)
		}
		if runtime.GOOS == "darwin" {
			h["cpu"] = darwinCPUPercent(darwinProcesses(s.ctx), runtime.NumCPU())
		} else {
			h["partial"] = true
		}
	}
	d := s.disk.get(30*time.Second, func() object {
		lines := strings.Split(sh(s.ctx, "df", "-k", "/"), "\n")
		d := object{"total": float64(0), "used": float64(0), "pct": float64(0)}
		if len(lines) > 1 {
			p := strings.Fields(lines[1])
			if len(p) > 4 {
				d["total"], d["used"], d["pct"] = number(p[1])*1024, number(p[2])*1024, number(strings.TrimSuffix(p[4], "%"))
			}
		}
		return d
	})
	h["diskTotal"], h["diskUsed"], h["diskPct"] = d["total"], d["used"], d["pct"]
	h["host"], _ = os.Hostname()
	h["t"] = time.Now().UnixMilli()
	pct := func(used, total float64) float64 {
		if total == 0 {
			return 0
		}
		return float64(int(100*used/total + .5))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.health = h
	for key, value := range map[string]float64{"cpu": h["cpu"].(float64), "mem": pct(h["memUsed"].(float64), h["memTotal"].(float64)), "swap": pct(h["swapUsed"].(float64), h["swapTotal"].(float64)), "load": h["load1"].(float64)} {
		s.hist[key] = append(s.hist[key], value)
		if len(s.hist[key]) > 120 {
			s.hist[key] = s.hist[key][1:]
		}
	}
}
func (s *healthSampler) snapshot() (object, map[string][]float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	hist := map[string][]float64{}
	for k, v := range s.hist {
		hist[k] = append([]float64{}, v...)
	}
	return s.health, hist
}
