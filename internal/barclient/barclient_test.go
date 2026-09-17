package barclient

import (
	"bytes"
	"context"
	"encoding/json"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDecodeStateUsageBoxesAndPeers(t *testing.T) {
	state, err := DecodeState(fixture(t, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	if state.Health.CPU != 12 || len(state.Agents) != 3 || len(state.Ports) != 5 {
		t.Fatalf("unexpected state: %+v", state)
	}
	if state.Agents[1].Status != "needs_you" {
		t.Fatalf("agent status = %q", state.Agents[1].Status)
	}

	usage, err := DecodeUsage(fixture(t, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	claude := usage.Providers["claude"]
	if claude.Quota == nil || claude.Quota.FiveHour == nil || claude.Quota.FiveHour.Pct != 20 {
		t.Fatalf("unexpected claude quota: %+v", claude.Quota)
	}
	if claude.Today.Tokens.Total() != 515654016 {
		t.Fatalf("claude tokens = %d", claude.Today.Tokens.Total())
	}

	boxes, err := DecodeUsageAll(fixture(t, "usage-all.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(boxes.Boxes) != 3 || !boxes.Boxes[2].Discovered || boxes.Boxes[2].Tag != "tailnet" {
		t.Fatalf("unexpected usage boxes: %+v", boxes.Boxes)
	}

	boxCards, err := DecodeBoxes(fixture(t, "boxes.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(boxCards) != 2 || !boxCards[1].Discovered || boxCards[1].AgentCount != 4 {
		t.Fatalf("unexpected box cards: %+v", boxCards)
	}

	peers, err := DecodePeers(fixture(t, "peers.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(peers) != 2 || peers[1].Name != "unmanaged" || peers[1].Boxdeck {
		t.Fatalf("unexpected peers: %+v", peers)
	}
}

func TestBuildMenuModelFromFixture(t *testing.T) {
	state, err := DecodeState(fixture(t, "state.json"))
	if err != nil {
		t.Fatal(err)
	}
	usage, err := DecodeUsage(fixture(t, "usage.json"))
	if err != nil {
		t.Fatal(err)
	}
	model := BuildMenu([]BoxSnapshot{{Name: "box", URL: "http://box:8100", State: state, Usage: usage, OK: true}})
	if model.IconState != IconAttention {
		t.Fatalf("icon state = %q", model.IconState)
	}
	want := []string{
		"cpu 12% mem 3.4/15 GB load 0.4",
		"agents: 2 working, 1 needs you",
		"ports: 5 open",
		"claude 5h 20% 7d 68%",
		"codex api key",
	}
	if got := model.Boxes[0].Lines; len(got) != len(want) {
		t.Fatalf("lines = %v", got)
	} else {
		for i := range want {
			if got[i].Title != want[i] {
				t.Errorf("line %d = %q, want %q", i, got[i].Title, want[i])
			}
		}
	}
}

func TestBuildMenuShowsNeedsYouAlertsAndEscalatesIcon(t *testing.T) {
	model := BuildMenu([]BoxSnapshot{{Name: "box", URL: "http://box:8100", Token: "token", OK: true, Alerts: []Alert{{ID: "a1", Title: "Approve", Severity: "critical", State: "open", Link: "#/agents?pane=qa", Actions: []AlertAction{{Label: "Approve", Method: "POST", Path: "/api/herd/keys", Body: map[string]any{"pane": "qa", "keys": "Enter"}}}}}}})
	if model.IconState != IconRust || len(model.Needs) != 1 {
		t.Fatalf("alert menu model = %+v", model)
	}
	if model.Needs[0].ActionLabel != "Approve" || model.Needs[0].Token != "token" {
		t.Fatalf("alert action = %+v", model.Needs[0])
	}
	if !strings.Contains(RenderText(model), "Needs you") || !strings.Contains(RenderText(model), "Approve") {
		t.Fatalf("alert menu text = %s", RenderText(model))
	}
}

func TestMenuModelUnreachableAndDiscovered(t *testing.T) {
	model := BuildMenu([]BoxSnapshot{
		{Name: "down", URL: "http://down:8100", Since: "2026-09-17T22:05:00Z"},
		{Name: "tail", URL: "http://tail:8100", Discovered: true, Tag: "tailnet", OK: true},
	})
	if model.IconState != IconRust {
		t.Fatalf("icon state = %q", model.IconState)
	}
	if model.Boxes[0].Lines[0].Title != "unreachable since 22:05" {
		t.Fatalf("unreachable line = %q", model.Boxes[0].Lines[0].Title)
	}
	if model.Boxes[1].Title != "tail [tailnet]" {
		t.Fatalf("discovered title = %q", model.Boxes[1].Title)
	}
}

func TestIconIsAn18PixelPNGWithStateTint(t *testing.T) {
	for _, state := range []IconState{IconHealthy, IconAttention, IconRust} {
		data, err := IconPNG(state)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) < 24 || string(data[:8]) != "\x89PNG\r\n\x1a\n" {
			t.Fatalf("state %q is not png", state)
		}
		decoded, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Bounds().Dx() != 18 || decoded.Bounds().Dy() != 18 {
			t.Fatalf("state %q is not 18 pixels", state)
		}
		if _, _, _, alpha := decoded.At(9, 9).RGBA(); alpha == 0 {
			t.Fatalf("state %q has no glyph at center", state)
		}
	}
}

func TestConfigTemplateAndPath(t *testing.T) {
	if got := ConfigPath("/home/alice", "linux"); got != "/home/alice/.config/boxdeck/bar.json" {
		t.Fatalf("linux path = %q", got)
	}
	if got := ConfigPath("C:/Users/alice", "windows"); got != "C:/Users/alice/AppData/Roaming/boxdeck/bar.json" {
		t.Fatalf("windows path = %q", got)
	}
	if err := ValidateConfig(Config{RefreshSec: 0}); err == nil {
		t.Fatal("expected refresh validation error")
	}
}

func TestFetchFleetAddsDiscoveredDevicesAndToleratesDiscovery404(t *testing.T) {
	state := string(fixture(t, "state.json"))
	usage := string(fixture(t, "usage.json"))
	boxes := string(fixture(t, "boxes.json"))
	usageAll := string(fixture(t, "usage-all.json"))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/state":
			_, _ = w.Write([]byte(state))
		case "/api/usage":
			_, _ = w.Write([]byte(usage))
		case "/api/boxes":
			_, _ = w.Write([]byte(boxes))
		case "/api/usage/all":
			_, _ = w.Write([]byte(usageAll))
		case "/api/net/peers":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	config := Config{Boxes: []BoxConfig{{Name: "box", URL: server.URL, Token: "box-token"}}, FleetToken: "fleet-token", RefreshSec: 30, OpenWith: "browser"}
	boxesResult := FetchFleet(context.Background(), config)
	if len(boxesResult) != 2 || !boxesResult[1].Discovered || boxesResult[1].Tag != "tailnet" {
		t.Fatalf("fleet boxes = %+v", boxesResult)
	}
	if boxesResult[1].Usage.Providers == nil {
		t.Fatal("discovered usage was not merged")
	}

	noDiscovery := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/state") || strings.HasPrefix(r.URL.Path, "/api/usage") {
			w.Header().Set("Content-Type", "application/json")
			if r.URL.Path == "/api/state" {
				_, _ = w.Write([]byte(state))
			} else {
				_, _ = w.Write([]byte(usage))
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer noDiscovery.Close()
	config.Boxes[0].URL = noDiscovery.URL
	if got := FetchFleet(context.Background(), config); len(got) != 1 {
		t.Fatalf("404 discovery result = %+v", got)
	}
	if !json.Valid([]byte(state)) {
		t.Fatal("state fixture is not valid JSON")
	}
}
