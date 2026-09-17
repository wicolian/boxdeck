package main

import (
	"fmt"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func fixtureSampler(t *testing.T) *procSampler {
	t.Helper()
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "block/sda"), 0700); err != nil {
		t.Fatal(err)
	}
	s := newProcSampler("testdata/proc/before", sys, "/home/test")
	s.users = map[string]string{"1000": "test-user"}
	return s
}
func TestProcFixtureRatesAndCPU(t *testing.T) {
	s := fixtureSampler(t)
	start := time.Unix(1000, 0)
	first := s.sample(start)
	if first.CPU != 0 || first.NetRX != 0 || !first.Available {
		t.Fatalf("first sample: %+v", first)
	}
	s.root = "testdata/proc/after"
	m := s.sample(start.Add(time.Second))
	if m.CPU != 50 || len(m.Cores) != 2 || m.Cores[0] != 60 || m.Cores[1] != 40 {
		t.Fatalf("cpu: %+v", m)
	}
	if m.NetRX != 1200 || m.NetTX != 800 || m.DiskRead != 5120 || m.DiskWrite != 10240 {
		t.Fatalf("rates: %+v", m)
	}
	if m.MemUsed != 4000*1024 || m.SwapUsed != 500*1024 || m.Load[0] != .5 {
		t.Fatalf("memory/load: %+v", m)
	}
	if len(m.TopCPU) != 1 || m.TopCPU[0].CPU != 25 || m.TopCPU[0].Name != "worker (io) done" || m.TopCPU[0].Mem != int64(300*os.Getpagesize()) || m.TopCPU[0].User != "test-user" || m.TopCPU[0].Age != 91 {
		t.Fatalf("process: %+v", m.TopCPU)
	}
}
func TestProcCounterResets(t *testing.T) {
	r, w := ioRates(map[string]ioCounter{"eth0": {2, 3}, "new": {500, 500}}, map[string]ioCounter{"eth0": {100, 100}}, 1)
	if r != 0 || w != 0 {
		t.Fatal("counter resets became large rates")
	}
	if _, err := parseProcStat("bad"); err == nil {
		t.Fatal("accepted bad stat")
	}
	cpus := parseCPUs("cpu 10 0 10 80 0 0 0 0 40 40\n")
	if cpus["cpu"].Total != 100 {
		t.Fatal("guest counted twice")
	}
}
func TestTopProcessesBounded(t *testing.T) {
	all := []procInfo{}
	for i := range 30 {
		all = append(all, procInfo{PID: i, CPU: float64(i), Mem: int64(30 - i)})
	}
	if p := sortedProcs(all, "cpu", 8); len(p) != 8 || p[0].PID != 29 {
		t.Fatal("CPU top eight")
	}
	if p := sortedProcs(all, "mem", 8); len(p) != 8 || p[0].PID != 0 {
		t.Fatal("memory top eight")
	}
}
func TestKillGuards(t *testing.T) {
	a := testApp(t)
	for _, pid := range []int{-1, 0, 1, os.Getpid()} {
		r := httptest.NewRequest("POST", "/api/proc/kill", strings.NewReader(fmt.Sprintf(`{"pid":%d,"signal":"SIGKILL"}`, pid)))
		r.SetBasicAuth(a.cfg.User, a.cfg.Password)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != 403 {
			t.Fatalf("pid %d got %d", pid, w.Code)
		}
	}
	r := httptest.NewRequest("POST", "/api/proc/kill", strings.NewReader(`{"pid":2,"signal":"SIGUSR1"}`))
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("unsupported signal accepted")
	}
}
func TestKillOwnChildAndIdentityGuard(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux process identity fixture")
	}
	a := testApp(t)
	child := exec.Command("sleep", "30")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer child.Process.Kill()
	send := func(start uint64) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/api/proc/kill", strings.NewReader(fmt.Sprintf(`{"pid":%d,"signal":"SIGTERM","startTicks":%d}`, child.Process.Pid, start)))
		r.SetBasicAuth(a.cfg.User, a.cfg.Password)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		return w
	}
	if w := send(1); w.Code != 409 {
		t.Fatal("stale process identity accepted")
	}
	st, err := parseProcStat(readFile(fmt.Sprintf("/proc/%d/stat", child.Process.Pid)))
	if err != nil {
		t.Fatal(err)
	}
	if w := send(st.Start); w.Code != 200 {
		t.Fatalf("stop child: %d %s", w.Code, w.Body.String())
	}
	if err = child.Wait(); err == nil {
		t.Fatal("expected signal exit")
	}
}
