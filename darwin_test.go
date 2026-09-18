package main

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestParseEtime(t *testing.T) {
	cases := map[string]int{"05": 5, "01:07": 67, "03:48:38": 13718, "2-03:48:38": 186518, "": 0, " 00:00 ": 0}
	for value, want := range cases {
		if got := parseEtime(value); got != want {
			t.Errorf("parseEtime(%q) = %d, want %d", value, got, want)
		}
	}
}

func TestParseDarwinPS(t *testing.T) {
	out := `    1     0 03:48:38   0.0  16384 root             Fri Sep 18 12:34:59 2026 /sbin/launchd
22967 21199 01:06:28   0.2 142016 kelashik         Fri Sep 18 15:33:10 2026 /Users/kelashik/.local/bin/claude --settings /var/x --model claude-opus-5
88838 87200    22:24 118.3 176928 kelashik         Fri Sep  8 15:58:33 2026 /Library/Developer/CoreSimulator/Profiles/Runtimes/iOS 26.2.simruntime/Contents/Resources/RuntimeRoot/usr/libexec/diagnosticd
bad line
`
	rows := parseDarwinPS(out)
	if len(rows) != 3 {
		t.Fatalf("got %d rows: %+v", len(rows), rows)
	}
	if rows[0].PID != 1 || rows[0].PPID != 0 || rows[0].Secs != 13718 || rows[0].User != "root" || rows[0].Args != "/sbin/launchd" || rows[0].RSS != 16384*1024 {
		t.Fatalf("launchd row %+v", rows[0])
	}
	claude := rows[1]
	if claude.PID != 22967 || claude.CPU != 0.2 || !strings.HasSuffix(claude.Args, "--model claude-opus-5") {
		t.Fatalf("claude row %+v", claude)
	}
	want := time.Date(2026, 9, 18, 15, 33, 10, 0, time.Local).Unix()
	if claude.Start != uint64(want) {
		t.Fatalf("start = %d, want %d", claude.Start, want)
	}
	if rows[2].Start == 0 || !strings.Contains(rows[2].Args, "iOS 26.2.simruntime") {
		t.Fatalf("single digit day row %+v", rows[2])
	}
	p := claude.process()
	if p.PID != 22967 || p.Secs != 3988 || p.Args != claude.Args {
		t.Fatalf("process() %+v", p)
	}
	cfg := config{}
	cfg.agentRE = mustAgentRE()
	agents := mergeAgents([]process{p}, nil, herdrState{}, cfg, func(int) string { return "" })
	if len(agents) != 1 || agents[0].Kind != "claude" || agents[0].Model != "claude-opus-5" {
		t.Fatalf("agents %+v", agents)
	}
}

func TestParseLsofListeners(t *testing.T) {
	out := `p645
cControlCenter
f9
n*:7000
f10
n*:7000
f11
n*:5000
p3773
cnode
f22
n127.0.0.1:3773
p8100
cboxdeck
f5
n[::1]:8100
f6
n127.0.0.1:8100
p9
cvpn
f3
n100.101.102.103:41641
`
	list := parseLsofListeners(out, []portNumber{5000})
	if len(list) != 3 {
		t.Fatalf("got %d listeners: %+v", len(list), list)
	}
	if list[0].Port != 3773 || list[0].Proc != "node" || list[0].PID != 3773 || list[0].Addr != "127.0.0.1" {
		t.Fatalf("node listener %+v", list[0])
	}
	if list[1].Port != 7000 || list[1].Proc != "ControlCenter" || list[1].Addr != "*" {
		t.Fatalf("control center listener %+v", list[1])
	}
	if list[2].Port != 8100 || list[2].Addr != "127.0.0.1" || list[2].Proc != "boxdeck" {
		t.Fatalf("ipv4 listener should win %+v", list[2])
	}
}

func TestParseLsofCWDs(t *testing.T) {
	out := "p7854\nfcwd\nn/Users/kelashik/codes/boxdeck\np72235\nfcwd\nn/Users/kelashik/codes\n"
	got := parseLsofCWDs(out)
	if got[7854] != "/Users/kelashik/codes/boxdeck" || got[72235] != "/Users/kelashik/codes" || len(got) != 2 {
		t.Fatalf("cwds %+v", got)
	}
	if len(darwinCWDs(context.Background(), nil)) != 0 || len(darwinCWDs(context.Background(), []int{0, -1})) != 0 {
		t.Fatal("empty pid lists must not call lsof")
	}
}

func TestParseEnvironmentValue(t *testing.T) {
	out := "/bin/zsh -il HOME=/Users/k HERDR_PANE_ID=pane-42 TERM=xterm-256color"
	if got := parseEnvironmentValue(out, "HERDR_PANE_ID"); got != "pane-42" {
		t.Fatalf("got %q", got)
	}
	if got := parseEnvironmentValue(out, "MISSING"); got != "" {
		t.Fatalf("got %q for a missing key", got)
	}
}

func TestParseSwapUsageAndBootTime(t *testing.T) {
	total, used, free := parseSwapUsage("total = 13312.00M  used = 12788.88M  free = 523.12M  (encrypted)")
	mib := float64(1 << 20)
	if total != 13312*mib || used != 12788.88*mib || free != 523.12*mib {
		t.Fatalf("swap %v %v %v", total, used, free)
	}
	if total, _, _ := parseSwapUsage("total = 2.00G used = 0.00K free = 2.00G"); total != 2<<30 {
		t.Fatalf("gigabyte swap = %v", total)
	}
	if got := parseBootTime("{ sec = 1789715099, usec = 479183 } Fri Sep 18 12:34:59 2026"); got != 1789715099 {
		t.Fatalf("boot time = %d", got)
	}
	if got := parseBootTime("garbage"); got != 0 {
		t.Fatalf("garbage boot time = %d", got)
	}
}

func TestParseVMStat(t *testing.T) {
	out := `Mach Virtual Memory Statistics: (page size of 16384 bytes)
Pages free:                               12345.
Pages active:                            400000.
Pages inactive:                          100000.
Pages speculative:                         1000.
Pages wired down:                        200000.
`
	pageSize, available := parseVMStat(out)
	if pageSize != 16384 || available != float64(12345+100000+1000)*16384 {
		t.Fatalf("page size %v available %v", pageSize, available)
	}
}

func TestParseNetstatIB(t *testing.T) {
	out := `Name       Mtu   Network       Address            Ipkts Ierrs     Ibytes    Opkts Oerrs     Obytes  Coll
lo0        16384 <Link#1>                         63250     0   48273619    63250     0   48273619     0
lo0        16384 127           127.0.0.1          63250     -   48273619    63250     -   48273619     -
en0        1500  <Link#14>     aa:bb:cc:dd:ee:ff 1000000     0 1234567890   500000     0  987654321     0
en0        1500  192.168.1     192.168.1.20      1000000     - 1234567890   500000     -  987654321     -
utun4      1500  <Link#25>                         2000     0     300000     1500     0     200000     0
`
	got := parseNetstatIB(out)
	if len(got) != 2 {
		t.Fatalf("interfaces %+v", got)
	}
	if got["en0"].Read != 1234567890 || got["en0"].Write != 987654321 {
		t.Fatalf("en0 %+v", got["en0"])
	}
	if got["utun4"].Read != 300000 || got["utun4"].Write != 200000 {
		t.Fatalf("utun4 without a link address %+v", got["utun4"])
	}
}

func TestDarwinCPUPercentAndProcessName(t *testing.T) {
	rows := []darwinProcess{{CPU: 50}, {CPU: 150}, {CPU: 0.5}}
	if got := darwinCPUPercent(rows, 4); got != 50 {
		t.Fatalf("cpu = %v", got)
	}
	if got := darwinCPUPercent(rows, 1); got != 100 {
		t.Fatalf("capped cpu = %v", got)
	}
	if got := darwinCPUPercent(nil, 0); got != 0 {
		t.Fatalf("empty cpu = %v", got)
	}
	exists := func(path string) bool {
		return path == "/sbin/launchd" || path == "/Library/Runtimes/iOS 26.2.simruntime/usr/libexec/diagnosticd" || path == "/usr/bin/python3"
	}
	cases := map[string]string{
		"/sbin/launchd": "launchd",
		"/Applications/Safari.app/Contents/MacOS/Safari -psn 1": "Safari",
		"claude --resume abc": "claude",
		"/Library/Runtimes/iOS 26.2.simruntime/usr/libexec/diagnosticd": "diagnosticd",
		"/Users/k/Applications/Codex Bar.app/Contents/MacOS/CodexBar":   "Codex Bar",
		"/usr/bin/python3 /Users/k/script.py --flag":                    "python3",
		"/nonexistent/tool with spaces -v":                              "tool with spaces",
		"":                                                              "",
	}
	for args, want := range cases {
		if got := darwinProcessNameOf(args, exists); got != want {
			t.Errorf("darwinProcessNameOf(%q) = %q, want %q", args, got, want)
		}
	}
	if darwinProcessName("/sbin/launchd") != "launchd" || darwinProcessName("/sbin/launchd") != "launchd" {
		t.Fatal("cached name lookup")
	}
}

func mustAgentRE() *regexp.Regexp {
	return regexp.MustCompile(`^(\S*/)?(claude|codex|aider|opencode|goose)(\s|$)`)
}

func TestShLenientKeepsOutputOnNonzeroExit(t *testing.T) {
	if got := sh(context.Background(), "sh", "-c", "echo partial; exit 1"); got != "" {
		t.Fatalf("sh returned %q for a failing command", got)
	}
	if got := shLenient(context.Background(), "sh", "-c", "echo partial; exit 1"); strings.TrimSpace(got) != "partial" {
		t.Fatalf("shLenient returned %q", got)
	}
	if got := shLenient(context.Background(), "definitely-not-a-command-xyz"); got != "" {
		t.Fatalf("missing command returned %q", got)
	}
}
