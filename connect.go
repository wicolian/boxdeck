package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

type connectSnippet struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Format  string `json:"format"`
	Content string `json:"content"`
}

type connectResponse struct {
	Endpoint string           `json:"endpoint"`
	AllowRun bool             `json:"allowRun"`
	Token    string           `json:"token,omitempty"`
	Label    string           `json:"label,omitempty"`
	Snippets []connectSnippet `json:"snippets"`
	PairURL  string           `json:"pairURL"`
	PairPNG  string           `json:"pairPNG"`
}

func connectEndpoint(r *http.Request) string {
	base := pairURL(r)
	return strings.TrimRight(base, "/") + "/mcp"
}

func connectSnippets(endpoint, token string) []connectSnippet {
	if token == "" {
		token = "<agent-token>"
	}
	base := strings.TrimSuffix(endpoint, "/mcp")
	jsonToken, _ := json.Marshal(token)
	curlBody := fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"curl","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}}}`)
	return []connectSnippet{
		{ID: "claude", Label: "Claude Code", Format: "shell", Content: fmt.Sprintf(`claude mcp add --transport http boxdeck %s --header "Authorization: Bearer %s"`, endpoint, token)},
		{ID: "codex", Label: "Codex", Format: "toml", Content: fmt.Sprintf(`[mcp_servers.boxdeck]
command = "boxdeck"
args = ["mcp", "--to", %s, "--token", %s]`, jsonToken, jsonToken)},
		{ID: "cursor", Label: "Cursor", Format: "json", Content: fmt.Sprintf(`{"mcpServers":{"boxdeck":{"url":%s,"headers":{"Authorization":"Bearer %s"}}}}`, quoteJSON(endpoint), quoteJSON(token))},
		{ID: "windsurf", Label: "Windsurf", Format: "json", Content: fmt.Sprintf(`{"mcpServers":{"boxdeck":{"serverUrl":%s,"headers":{"Authorization":"Bearer %s"}}}}`, quoteJSON(endpoint), quoteJSON(token))},
		{ID: "curl", Label: "curl", Format: "shell", Content: fmt.Sprintf(`curl -sS -X POST %s -H 'Accept: application/json' -H 'Content-Type: application/json' -H 'Authorization: Bearer %s' -H 'MCP-Protocol-Version: 2026-07-28' -H 'Mcp-Method: tools/list' -d '%s'`, endpoint, token, curlBody)},
		{ID: "laptop", Label: "Any laptop", Format: "shell", Content: fmt.Sprintf(`boxdeck mcp --to %s --token %s`, base, token)},
	}
}

func quoteJSON(value string) string {
	b, _ := json.Marshal(value)
	return string(b)
}

func (a *app) connectSnapshot(r *http.Request, token string) connectResponse {
	if token == "" {
		for i := len(a.cfg.Tokens) - 1; i >= 0; i-- {
			if tokenLabel(a.cfg, a.cfg.Tokens[i]) == "agent" {
				token = a.cfg.Tokens[i]
				break
			}
		}
	}
	endpoint := connectEndpoint(r)
	return connectResponse{
		Endpoint: endpoint,
		AllowRun: a.cfg.AllowRun,
		Token:    token,
		Label: func() string {
			if token == "" {
				return ""
			}
			return "agent"
		}(),
		Snippets: connectSnippets(endpoint, token),
		PairURL:  strings.TrimRight(pairURL(r), "/") + "/api/pair.json",
		PairPNG:  strings.TrimRight(pairURL(r), "/") + "/api/pair",
	}
}

func (a *app) connectAPI(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		jsonReply(w, http.StatusOK, a.connectSnapshot(r, ""))
	case http.MethodPost:
		var body struct {
			NewToken *bool `json:"newToken"`
			AllowRun *bool `json:"allowRun"`
		}
		if !decodeJSONBody(w, r, &body, 4096) {
			return
		}
		a.tokenMu.Lock()
		defer a.tokenMu.Unlock()
		if body.AllowRun != nil {
			a.cfg.AllowRun = *body.AllowRun
			if a.cfg.path != "" {
				if err := saveConfig(a.cfg); err != nil {
					jsonReply(w, http.StatusInternalServerError, object{"error": "Could not save allowRun"})
					return
				}
			}
		}
		if body.NewToken == nil || *body.NewToken {
			token, err := randomToken()
			if err != nil {
				jsonReply(w, http.StatusInternalServerError, object{"error": "Could not create an agent token"})
				return
			}
			addLabeledToken(&a.cfg, token, "agent")
			if a.cfg.path != "" {
				if err := saveConfig(a.cfg); err != nil {
					a.cfg.Tokens = a.cfg.Tokens[:len(a.cfg.Tokens)-1]
					delete(a.cfg.TokenLabels, token)
					jsonReply(w, http.StatusInternalServerError, object{"error": "Could not save the agent token"})
					return
				}
			}
			jsonReply(w, http.StatusCreated, a.connectSnapshot(r, token))
			return
		}
		jsonReply(w, http.StatusOK, a.connectSnapshot(r, ""))
	default:
		w.Header().Set("Allow", "GET, POST")
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET or POST for Connect"})
	}
}
