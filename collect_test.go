package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSSFixture(t *testing.T) {
	b, err := os.ReadFile("testdata/ss.txt")
	if err != nil {
		t.Fatal(err)
	}
	p := parseSS(string(b), []portNumber{22})
	if len(p) != 3 || p[0].Port != 3001 || p[0].Proc != "node" || p[0].PID != 120 || p[1].Port != 8384 || p[2].Port != 45000 {
		t.Fatalf("ports: %+v", p)
	}
	for _, ip := range []string{"100.64.0.1", "100.127.255.255"} {
		if !isTailnetIP(ip) {
			t.Fatal(ip)
		}
	}
	for _, ip := range []string{"100.63.0.1", "100.128.0.1", "127.0.0.1"} {
		if isTailnetIP(ip) {
			t.Fatal(ip)
		}
	}
}
func TestHerdrFixtureAndProcessMerge(t *testing.T) {
	b, err := os.ReadFile("testdata/herdr.json")
	if err != nil {
		t.Fatal(err)
	}
	h, err := parseHerdr(b)
	if err != nil || !h.Running || h.Workspaces[0].Name != "demo" || h.Workspaces[0].Tabs != 2 || h.Agents[0].Tokens["limit"] != "5h 88%" {
		t.Fatalf("snapshot: %+v, %v", h, err)
	}
	c := config{home: "/home/demo", agentRE: regexp.MustCompile(`^(\S*/)?(claude|codex)(\s|$)`)}
	all := []process{{PID: 1, Args: "claude", Secs: 50}, {PID: 2, Args: "claude", Secs: 49}, {PID: 3, Args: "codex --model gpt-test", Secs: 20}}
	list := mergeAgents(all, nil, h, c, func(pid int) string {
		if pid == 1 || pid == 2 {
			return "w1:p1"
		}
		return ""
	})
	if len(list) != 3 || list[0].PID != 1 || list[0].Title != "Fix login" || list[0].CWD != "~/project" || list[1].Model != "gpt-test" || list[2].PaneID != "w1:p2" {
		t.Fatalf("merge: %+v", list)
	}
	for _, bad := range []string{`{}`, `{"error":{"message":"absent"}}`, `{"result":{"type":"pong"}}`} {
		if _, err := parseHerdr([]byte(bad)); err == nil {
			t.Fatal("bad snapshot accepted")
		}
	}
}
func TestMemoAndSampling(t *testing.T) {
	var m memo[int]
	var calls atomic.Int32
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() { defer wg.Done(); m.get(time.Hour, func() int { calls.Add(1); return 42 }) }()
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatal("concurrent polls repeated shell-out")
	}
	if sampleDelay(false) != 30*time.Second || sampleDelay(true) != 3*time.Second {
		t.Fatal("sampling cadence")
	}
	s := healthSampler{ctx: context.Background()}
	if s.watching() {
		t.Fatal("idle watcher")
	}
	s.lastPoll.Store(time.Now().UnixMilli())
	if !s.watching() {
		t.Fatal("active watcher")
	}
	s.lastPoll.Store(time.Now().Add(-16 * time.Second).UnixMilli())
	if s.watching() {
		t.Fatal("watcher never expired")
	}
}
func TestPSParser(t *testing.T) {
	p := parsePS(" 123 1 44 1.2 1024 /usr/bin/codex --model=test\n")
	if len(p) != 1 || p[0].RSS != 1048576 || p[0].CPU != 1.2 {
		t.Fatalf("process: %+v", p)
	}
}

func TestMirrorBindFailureAndTerminalExclusion(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	m := mirrorManager{ctx: context.Background(), cfg: config{Port: portNumber(port), TTYDPort: 7682, Mirror: true, MirrorBind: "127.0.0.1"}, entries: map[int]mirrorEntry{}}
	defer m.close()
	m.sync([]portInfo{{Port: 7682}})
	ports, errors := m.snapshot()
	if len(ports) != 0 || len(errors) != 1 || !strings.Contains(errors[0], fmt.Sprint(port)) || !strings.Contains(errors[0], "address already in use") {
		t.Fatalf("missing actionable bind error: %v", errors)
	}
	if _, ok := m.entries[7682]; ok {
		t.Fatal("unauthenticated terminal was mirrored")
	}
}
