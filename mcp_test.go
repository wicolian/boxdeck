package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func mcpTestRequest(t *testing.T, a *app, body string, version string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	r.Header.Set("Authorization", "Bearer phase5-mcp-token")
	r.Header.Set("Accept", "application/json, text/event-stream")
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("MCP-Protocol-Version", version)
	var message struct {
		Method string `json:"method"`
		Params struct {
			Name string `json:"name"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &message); err != nil {
		t.Fatal(err)
	}
	r.Header.Set("Mcp-Method", message.Method)
	if message.Method == "tools/call" {
		r.Header.Set("Mcp-Name", message.Params.Name)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	return w
}

func TestMCPModernRoundTripAndToolResult(t *testing.T) {
	a := testApp(t)
	a.cfg.Tokens = []string{"phase5-mcp-token"}
	path := filepath.Join(a.cfg.home, "mcp-test.txt")
	if err := os.WriteFile(path, []byte("hello from mcp"), 0600); err != nil {
		t.Fatal(err)
	}
	meta := `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`
	init := mcpTestRequest(t, a, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+meta+`}}`, "2026-07-28")
	if init.Code != http.StatusOK || !strings.Contains(init.Body.String(), `"resultType":"complete"`) {
		t.Fatalf("discover: %d %s", init.Code, init.Body.String())
	}
	list := mcpTestRequest(t, a, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{`+meta+`}}`, "2026-07-28")
	if list.Code != http.StatusOK || !strings.Contains(list.Body.String(), `"name":"box_state"`) || !strings.Contains(list.Body.String(), `"ttlMs"`) || !strings.Contains(list.Body.String(), `"cacheScope":"private"`) {
		t.Fatalf("tools/list: %d %s", list.Code, list.Body.String())
	}
	call := mcpTestRequest(t, a, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"file_read","arguments":{"path":"/mcp-test.txt","max":100},`+meta+`}}`, "2026-07-28")
	if call.Code != http.StatusOK || !strings.Contains(call.Body.String(), "hello from mcp") {
		t.Fatalf("file_read: %d %s", call.Code, call.Body.String())
	}
	second := mcpTestRequest(t, a, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"box_state","arguments":{},`+meta+`}}`, "2026-07-28")
	if second.Code != http.StatusOK {
		t.Fatalf("stateless box_state: %d %s", second.Code, second.Body.String())
	}
}

func TestMCPLegacyInitializeAndSession(t *testing.T) {
	a := testApp(t)
	a.cfg.Tokens = []string{"phase5-mcp-token"}
	init := mcpTestRequest(t, a, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"old","version":"1"}}}`, "2025-06-18")
	if init.Code != http.StatusOK || init.Header().Get("Mcp-Session-Id") == "" || !strings.Contains(init.Body.String(), `"protocolVersion":"2025-06-18"`) {
		t.Fatalf("initialize: %d %s", init.Code, init.Body.String())
	}
	r := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	r.Header.Set("Authorization", "Bearer phase5-mcp-token")
	r.Header.Set("MCP-Protocol-Version", "2025-06-18")
	r.Header.Set("Mcp-Method", "tools/list")
	r.Header.Set("Mcp-Session-Id", init.Header().Get("Mcp-Session-Id"))
	r.Header.Set("Accept", "application/json")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"name":"box_state"`) {
		t.Fatalf("legacy tools/list: %d %s", w.Code, w.Body.String())
	}
}

func TestMCPAuthOriginAndRunVisibility(t *testing.T) {
	a := testApp(t)
	a.cfg.Tokens = []string{"phase5-mcp-token"}
	body := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"test","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`
	unauth := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	unauth.Header.Set("MCP-Protocol-Version", "2026-07-28")
	unauth.Header.Set("Mcp-Method", "tools/list")
	unauth.Header.Set("Accept", "application/json")
	unauthResponse := httptest.NewRecorder()
	a.ServeHTTP(unauthResponse, unauth)
	if unauthResponse.Code != http.StatusUnauthorized || !strings.Contains(unauthResponse.Body.String(), `"error"`) {
		t.Fatalf("unauthorized: %d %s", unauthResponse.Code, unauthResponse.Body.String())
	}
	origin := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	origin.Header.Set("Authorization", "Bearer phase5-mcp-token")
	origin.Header.Set("MCP-Protocol-Version", "2026-07-28")
	origin.Header.Set("Mcp-Method", "tools/list")
	origin.Header.Set("Origin", "https://attacker.invalid")
	originResponse := httptest.NewRecorder()
	a.ServeHTTP(originResponse, origin)
	if originResponse.Code != http.StatusForbidden {
		t.Fatalf("origin rejection: %d %s", originResponse.Code, originResponse.Body.String())
	}
	list := mcpTestRequest(t, a, body, "2026-07-28")
	if strings.Contains(list.Body.String(), `"name":"run"`) {
		t.Fatal("run is exposed while allowRun is false")
	}
	a.cfg.AllowRun = true
	list = mcpTestRequest(t, a, body, "2026-07-28")
	if !strings.Contains(list.Body.String(), `"name":"run"`) {
		t.Fatal("run is hidden while allowRun is true")
	}
}

func TestMCPStdioForwardUsesHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer bridge-token" || r.Header.Get("Mcp-Method") != "ping" {
			t.Fatalf("bridge headers: auth=%q method=%q", r.Header.Get("Authorization"), r.Header.Get("Mcp-Method"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":9,"result":{}}`))
	}))
	defer server.Close()
	var out bytes.Buffer
	if err := mcpBridge(strings.NewReader(`{"jsonrpc":"2.0","id":9,"method":"ping","params":{}}
`), &out, server.URL, "bridge-token"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"id":9`) {
		t.Fatalf("bridge output: %s", out.String())
	}
}

func TestConnectMintsLabeledTokenAndTogglesRun(t *testing.T) {
	a := testApp(t)
	request := httptest.NewRequest(http.MethodPost, "/api/connect", strings.NewReader(`{"newToken":true}`))
	request.SetBasicAuth(a.cfg.User, a.cfg.Password)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	a.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("connect token: %d %s", response.Code, response.Body.String())
	}
	var created connectResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Token == "" || tokenLabel(a.cfg, created.Token) != "agent" || !strings.Contains(created.Snippets[0].Content, created.Token) {
		t.Fatalf("created connect response: %+v", created)
	}
	get := httptest.NewRequest(http.MethodGet, "/api/connect", nil)
	get.SetBasicAuth(a.cfg.User, a.cfg.Password)
	getResponse := httptest.NewRecorder()
	a.ServeHTTP(getResponse, get)
	if !strings.Contains(getResponse.Body.String(), created.Token) {
		t.Fatal("GET /api/connect did not return the active agent snippets")
	}
	toggle := httptest.NewRequest(http.MethodPost, "/api/connect", strings.NewReader(`{"allowRun":true,"newToken":false}`))
	toggle.SetBasicAuth(a.cfg.User, a.cfg.Password)
	toggle.Header.Set("Content-Type", "application/json")
	toggleResponse := httptest.NewRecorder()
	a.ServeHTTP(toggleResponse, toggle)
	if toggleResponse.Code != http.StatusOK || !a.cfg.AllowRun {
		t.Fatalf("allowRun toggle: %d %s", toggleResponse.Code, toggleResponse.Body.String())
	}
}

func TestMCPListResultsAreObjects(t *testing.T) {
	r := mcpJSONResult([]string{"a", "b"})
	m, ok := r.Structured.(object)
	if !ok || m["count"] != 2 {
		t.Fatalf("list structuredContent = %#v, want an object with count 2", r.Structured)
	}
	if _, ok := mcpJSONResult(object{"x": 1}).Structured.(object); !ok {
		t.Fatal("object results must pass through")
	}
	if m, ok := mcpJSONResult(nil).Structured.(object); !ok || m["count"] != 0 {
		t.Fatalf("nil result = %#v", mcpJSONResult(nil).Structured)
	}
}
