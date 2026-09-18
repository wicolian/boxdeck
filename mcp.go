package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	mcpCurrentVersion     = "2026-07-28"
	mcpServerName         = "boxdeck"
	mcpHeaderMismatchCode = -32020
	mcpUnsupported        = -32022
)

var mcpSupportedVersions = []string{mcpCurrentVersion, "2025-11-25", "2025-06-18", "2025-03-26"}

type mcpRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type mcpRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

type mcpRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *mcpRPCError    `json:"error,omitempty"`
}

type mcpToolContent struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	Data     string `json:"data,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
}

type mcpToolResult struct {
	ResultType string           `json:"resultType"`
	Content    []mcpToolContent `json:"content"`
	Structured any              `json:"structuredContent,omitempty"`
	IsError    bool             `json:"isError,omitempty"`
}

type mcpTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Call        func(context.Context, map[string]any) (mcpToolResult, error)
}

type mcpSession struct {
	Version string
}

func mcpID(id json.RawMessage) json.RawMessage {
	if len(id) == 0 {
		return json.RawMessage("null")
	}
	return id
}

func mcpErrorResponse(id json.RawMessage, code int, message string, data any) mcpRPCResponse {
	return mcpRPCResponse{JSONRPC: "2.0", ID: mcpID(id), Error: &mcpRPCError{Code: code, Message: message, Data: data}}
}

func mcpResultResponse(id json.RawMessage, result any) mcpRPCResponse {
	return mcpRPCResponse{JSONRPC: "2.0", ID: mcpID(id), Result: result}
}

func mcpWrite(w http.ResponseWriter, status int, response mcpRPCResponse, sse bool) {
	b, err := json.Marshal(response)
	if err != nil {
		status = http.StatusInternalServerError
		b = []byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32603,"message":"Could not encode response"}}`)
		sse = false
	}
	w.Header().Set("Cache-Control", "no-store")
	if sse && status >= 200 && status < 300 {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(status)
		_, _ = fmt.Fprintf(w, "event: message\ndata: %s\n\n", b)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func mcpNoBody(w http.ResponseWriter, status int) {
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
}

func mcpParams(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return map[string]json.RawMessage{}, nil
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(raw, &params); err != nil || params == nil {
		return nil, errors.New("params must be an object")
	}
	return params, nil
}

func mcpMeta(params map[string]json.RawMessage) (map[string]json.RawMessage, string) {
	var meta map[string]json.RawMessage
	if raw := params["_meta"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	var version string
	if raw := meta["io.modelcontextprotocol/protocolVersion"]; len(raw) > 0 {
		_ = json.Unmarshal(raw, &version)
	}
	return meta, version
}

func mcpDecodeHeader(value string) (string, error) {
	if strings.HasPrefix(value, "=?base64?") && strings.HasSuffix(value, "?=") {
		encoded := strings.TrimSuffix(strings.TrimPrefix(value, "=?base64?"), "?=")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return "", errors.New("invalid base64 header value")
		}
		return string(decoded), nil
	}
	for _, r := range value {
		if r < 0x20 && r != '\t' || r > 0x7e {
			return "", errors.New("header value contains invalid characters")
		}
	}
	return value, nil
}

func mcpHeaderMismatch(id json.RawMessage, message string) mcpRPCResponse {
	return mcpErrorResponse(id, mcpHeaderMismatchCode, "Header mismatch: "+message, nil)
}

func mcpUnsupportedResponse(id json.RawMessage, requested string) mcpRPCResponse {
	return mcpErrorResponse(id, mcpUnsupported, "Unsupported protocol version", map[string]any{"supported": mcpSupportedVersions, "requested": requested})
}

func mcpBodyVersion(req mcpRPCRequest, params map[string]json.RawMessage) (string, error) {
	_, metaVersion := mcpMeta(params)
	if metaVersion != "" {
		return metaVersion, nil
	}
	if req.Method == "initialize" {
		var version string
		_ = json.Unmarshal(params["protocolVersion"], &version)
		return version, nil
	}
	return "", nil
}

func mcpVersion(req mcpRPCRequest, r *http.Request, params map[string]json.RawMessage) (string, bool, *mcpRPCResponse) {
	headerVersion := strings.TrimSpace(r.Header.Get("MCP-Protocol-Version"))
	bodyVersion, err := mcpBodyVersion(req, params)
	if err != nil {
		response := mcpErrorResponse(req.ID, -32602, "Invalid protocol metadata", nil)
		return "", false, &response
	}
	if headerVersion != "" && bodyVersion != "" && headerVersion != bodyVersion {
		response := mcpHeaderMismatch(req.ID, "MCP-Protocol-Version header does not match the request metadata")
		return "", false, &response
	}
	version := headerVersion
	if version == "" {
		version = bodyVersion
	}
	if version == "" {
		version = "2025-03-26"
	}
	for _, supported := range mcpSupportedVersions {
		if version == supported {
			return version, version == mcpCurrentVersion, nil
		}
	}
	response := mcpUnsupportedResponse(req.ID, version)
	return version, false, &response
}

func mcpValidateHeaders(req mcpRPCRequest, r *http.Request, params map[string]json.RawMessage, modern bool) *mcpRPCResponse {
	methodHeader := strings.TrimSpace(r.Header.Get("Mcp-Method"))
	if modern {
		if methodHeader == "" {
			response := mcpHeaderMismatch(req.ID, "Mcp-Method header is required")
			return &response
		}
		decoded, err := mcpDecodeHeader(methodHeader)
		if err != nil || decoded != req.Method {
			response := mcpHeaderMismatch(req.ID, "Mcp-Method header does not match the request method")
			return &response
		}
		_, metaVersion := mcpMeta(params)
		if metaVersion == "" {
			response := mcpErrorResponse(req.ID, -32602, "Request metadata must include protocolVersion", nil)
			return &response
		}
		if req.Method == "tools/call" {
			var callParams struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(req.Params, &callParams)
			nameHeader := r.Header.Get("Mcp-Name")
			if nameHeader == "" {
				response := mcpHeaderMismatch(req.ID, "Mcp-Name header is required for tools/call")
				return &response
			}
			decodedName, err := mcpDecodeHeader(nameHeader)
			if err != nil || decodedName != callParams.Name {
				response := mcpHeaderMismatch(req.ID, "Mcp-Name header does not match params.name")
				return &response
			}
		}
		return nil
	}
	if methodHeader != "" {
		decoded, err := mcpDecodeHeader(methodHeader)
		if err != nil || decoded != req.Method {
			response := mcpHeaderMismatch(req.ID, "Mcp-Method header does not match the request method")
			return &response
		}
	}
	if nameHeader := r.Header.Get("Mcp-Name"); nameHeader != "" && req.Method == "tools/call" {
		var callParams struct {
			Name string `json:"name"`
		}
		_ = json.Unmarshal(req.Params, &callParams)
		decoded, err := mcpDecodeHeader(nameHeader)
		if err != nil || decoded != callParams.Name {
			response := mcpHeaderMismatch(req.ID, "Mcp-Name header does not match params.name")
			return &response
		}
	}
	return nil
}

func (a *app) mcpHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") != "" && !sameOrigin(r) {
		response := mcpErrorResponse(nil, -32000, "Forbidden origin", nil)
		mcpWrite(w, http.StatusForbidden, response, false)
		return
	}
	if !a.authed(r) {
		response := mcpErrorResponse(nil, -32000, "Unauthorized", nil)
		mcpWrite(w, http.StatusUnauthorized, response, false)
		return
	}
	switch r.Method {
	case http.MethodGet:
		a.mcpLegacySSE(w, r)
		return
	case http.MethodDelete:
		a.mcpDelete(w, r)
		return
	case http.MethodPost:
	default:
		w.Header().Set("Allow", "POST, GET, DELETE")
		response := mcpErrorResponse(nil, -32600, "Method not allowed", nil)
		mcpWrite(w, http.StatusMethodNotAllowed, response, false)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 2<<20)
	var req mcpRPCRequest
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil || req.JSONRPC != "2.0" || req.Method == "" {
		response := mcpErrorResponse(nil, -32600, "Invalid JSON-RPC request", nil)
		mcpWrite(w, http.StatusBadRequest, response, false)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		response := mcpErrorResponse(req.ID, -32600, "Batch JSON-RPC messages are not supported", nil)
		mcpWrite(w, http.StatusBadRequest, response, false)
		return
	}
	params, err := mcpParams(req.Params)
	if err != nil {
		response := mcpErrorResponse(req.ID, -32602, err.Error(), nil)
		mcpWrite(w, http.StatusBadRequest, response, false)
		return
	}
	protocolVersion, modern, versionError := mcpVersion(req, r, params)
	if versionError != nil {
		mcpWrite(w, http.StatusBadRequest, *versionError, false)
		return
	}
	if headerError := mcpValidateHeaders(req, r, params, modern); headerError != nil {
		mcpWrite(w, http.StatusBadRequest, *headerError, false)
		return
	}
	if req.Method == "initialize" && modern {
		result := map[string]any{"resultType": "complete", "protocolVersion": mcpCurrentVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}, "logging": map[string]any{}}, "serverInfo": map[string]any{"name": mcpServerName, "version": version}}
		mcpWrite(w, http.StatusOK, mcpResultResponse(req.ID, result), acceptsMCPEventStream(r))
		return
	}
	if req.Method == "initialize" {
		session, err := randomToken()
		if err != nil {
			response := mcpErrorResponse(req.ID, -32603, "Could not create MCP session", nil)
			mcpWrite(w, http.StatusInternalServerError, response, false)
			return
		}
		a.mcpStoreSession(session, mcpSession{Version: protocolVersion})
		w.Header().Set("Mcp-Session-Id", session)
		result := map[string]any{"protocolVersion": protocolVersion, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}, "logging": map[string]any{}}, "serverInfo": map[string]any{"name": mcpServerName, "version": version}}
		mcpWrite(w, http.StatusOK, mcpResultResponse(req.ID, result), acceptsMCPEventStream(r))
		return
	}
	if session := r.Header.Get("Mcp-Session-Id"); session != "" && !modern {
		if stored, ok := a.mcpLoadSession(session); ok && stored.Version != protocolVersion {
			response := mcpHeaderMismatch(req.ID, "Mcp-Session-Id uses a different protocol version")
			mcpWrite(w, http.StatusBadRequest, response, false)
			return
		}
	}
	if req.ID == nil {
		if req.Method == "notifications/initialized" || req.Method == "notifications/cancelled" {
			mcpNoBody(w, http.StatusAccepted)
			return
		}
		response := mcpErrorResponse(nil, -32600, "Notifications are not supported for this method", nil)
		mcpWrite(w, http.StatusBadRequest, response, false)
		return
	}
	response, status := a.mcpDispatch(r.Context(), req)
	mcpWrite(w, status, response, acceptsMCPEventStream(r))
}

func acceptsMCPEventStream(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Accept")), "text/event-stream")
}

func (a *app) mcpDelete(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("MCP-Protocol-Version") == mcpCurrentVersion {
		response := mcpErrorResponse(nil, -32601, "GET and DELETE are not part of modern Streamable HTTP", nil)
		mcpWrite(w, http.StatusMethodNotAllowed, response, false)
		return
	}
	session := r.Header.Get("Mcp-Session-Id")
	if session != "" {
		a.mcpDeleteSession(session)
	}
	mcpNoBody(w, http.StatusNoContent)
}

func (a *app) mcpLegacySSE(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("MCP-Protocol-Version") == mcpCurrentVersion {
		response := mcpErrorResponse(nil, -32601, "GET is not part of modern Streamable HTTP", nil)
		mcpWrite(w, http.StatusMethodNotAllowed, response, false)
		return
	}
	if a.alerts == nil {
		http.Error(w, "alerts are not available", http.StatusServiceUnavailable)
		return
	}
	ch, backlog, stop := a.alerts.subscribe(0)
	defer stop()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming is unavailable", http.StatusInternalServerError)
		return
	}
	writeEvent := func(event alertEvent) bool {
		data, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info", "logger": "boxdeck", "data": event}})
		if err != nil {
			return false
		}
		_, err = fmt.Fprintf(w, "data: %s\n\n", data)
		if err == nil {
			flusher.Flush()
		}
		return err == nil
	}
	for _, event := range backlog {
		if !writeEvent(event) {
			return
		}
	}
	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-ch:
			if !ok || !writeEvent(event) {
				return
			}
		}
	}
}

func (a *app) mcpStoreSession(id string, session mcpSession) {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	if a.mcpSessions == nil {
		a.mcpSessions = map[string]mcpSession{}
	}
	a.mcpSessions[id] = session
}

func (a *app) mcpLoadSession(id string) (mcpSession, bool) {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	session, ok := a.mcpSessions[id]
	return session, ok
}

func (a *app) mcpDeleteSession(id string) {
	a.mcpMu.Lock()
	defer a.mcpMu.Unlock()
	delete(a.mcpSessions, id)
}

func (a *app) mcpDispatch(ctx context.Context, req mcpRPCRequest) (mcpRPCResponse, int) {
	switch req.Method {
	case "server/discover":
		return mcpResultResponse(req.ID, map[string]any{"resultType": "complete", "protocolVersions": mcpSupportedVersions, "capabilities": map[string]any{"tools": map[string]any{"listChanged": false}, "logging": map[string]any{}}, "serverInfo": map[string]any{"name": mcpServerName, "version": version}}), http.StatusOK
	case "tools/list":
		list := make([]map[string]any, 0)
		for _, tool := range a.mcpTools() {
			list = append(list, map[string]any{"name": tool.Name, "description": tool.Description, "inputSchema": tool.InputSchema})
		}
		return mcpResultResponse(req.ID, map[string]any{"resultType": "complete", "tools": list, "ttlMs": 30000, "cacheScope": "private"}), http.StatusOK
	case "tools/call":
		var call struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &call); err != nil || strings.TrimSpace(call.Name) == "" {
			return mcpResultResponse(req.ID, mcpToolError("name and arguments are required")), http.StatusOK
		}
		if call.Arguments == nil {
			call.Arguments = map[string]any{}
		}
		for _, tool := range a.mcpTools() {
			if tool.Name != call.Name {
				continue
			}
			result, err := tool.Call(ctx, call.Arguments)
			if err != nil {
				return mcpResultResponse(req.ID, mcpToolError(err.Error())), http.StatusOK
			}
			return mcpResultResponse(req.ID, result), http.StatusOK
		}
		return mcpResultResponse(req.ID, mcpToolError("Unknown tool: "+call.Name)), http.StatusOK
	case "ping":
		return mcpResultResponse(req.ID, map[string]any{"resultType": "complete"}), http.StatusOK
	default:
		return mcpErrorResponse(req.ID, -32601, "Method not found: "+req.Method, nil), http.StatusNotFound
	}
}

func mcpToolError(message string) mcpToolResult {
	return mcpToolResult{ResultType: "complete", Content: []mcpToolContent{{Type: "text", Text: message}}, IsError: true}
}

func mcpJSONResult(value any) mcpToolResult {
	b, _ := json.Marshal(value)
	// structuredContent must be a JSON object (clients such as Claude Code reject an array as a
	// malformed result), so a list is wrapped as {"items": [...], "count": n}.
	structured := value
	if rv := reflect.ValueOf(value); value == nil || rv.Kind() == reflect.Slice || rv.Kind() == reflect.Array {
		n := 0
		if value != nil {
			n = rv.Len()
		}
		structured = object{"items": value, "count": n}
	} else if rv.Kind() != reflect.Map && rv.Kind() != reflect.Struct && !(rv.Kind() == reflect.Ptr && rv.Elem().Kind() == reflect.Struct) {
		structured = object{"value": value}
	}
	return mcpToolResult{ResultType: "complete", Content: []mcpToolContent{{Type: "text", Text: string(b)}}, Structured: structured}
}

func mcpSchema(properties map[string]any, required ...string) map[string]any {
	schema := map[string]any{"type": "object", "properties": properties}
	if len(required) > 0 {
		schema["required"] = required
	}
	return schema
}

func mcpString(description string) map[string]any {
	return map[string]any{"type": "string", "description": description}
}
func mcpInteger(description string) map[string]any {
	return map[string]any{"type": "integer", "description": description}
}
func mcpNumber(description string) map[string]any {
	return map[string]any{"type": "number", "description": description}
}

func (a *app) mcpTools() []mcpTool {
	empty := func() map[string]any { return mcpSchema(map[string]any{}) }
	tools := []mcpTool{
		{Name: "box_state", Description: "Read the complete live state of this box.", InputSchema: empty(), Call: func(context.Context, map[string]any) (mcpToolResult, error) { return mcpJSONResult(a.state()), nil }},
		{Name: "box_ports", Description: "List listening ports and their owning processes.", InputSchema: empty(), Call: func(context.Context, map[string]any) (mcpToolResult, error) {
			return mcpJSONResult(a.collect.getPorts()), nil
		}},
		{Name: "box_processes", Description: "List box processes with optional sorting, limit, and text filter.", InputSchema: mcpSchema(map[string]any{"sort": mcpString("cpu, mem, age, or pid"), "n": mcpInteger("Maximum number of processes"), "filter": mcpString("Case-insensitive text filter")}), Call: a.mcpProcesses},
		{Name: "box_process_kill", Description: "Send SIGTERM or SIGKILL to a process after safety checks.", InputSchema: mcpSchema(map[string]any{"pid": mcpInteger("Process ID"), "signal": mcpString("SIGTERM or SIGKILL"), "startTicks": mcpInteger("Optional process start counter")}, "pid"), Call: a.mcpProcessKill},
		{Name: "box_agents", Description: "List coding agents merged with tmux and herdr state.", InputSchema: empty(), Call: func(context.Context, map[string]any) (mcpToolResult, error) {
			return mcpJSONResult(mergeAgents(a.collect.getProcesses(), a.collect.getPanes(), a.collect.getHerdr(), a.cfg, paneFromEnvironment)), nil
		}},
		{Name: "agent_read", Description: "Read the latest output from an agent pane.", InputSchema: mcpSchema(map[string]any{"pane": mcpString("Herdr pane ID or tmux target"), "lines": mcpInteger("Number of lines, from 1 to 400")}, "pane"), Call: a.mcpAgentRead},
		{Name: "agent_prompt", Description: "Send a prompt to an agent pane.", InputSchema: mcpSchema(map[string]any{"pane": mcpString("Herdr pane ID or tmux target"), "text": mcpString("Prompt text")}, "pane", "text"), Call: a.mcpAgentPrompt},
		{Name: "agent_keys", Description: "Send a safe key such as Enter, y, n, Tab, or Esc to an agent pane.", InputSchema: mcpSchema(map[string]any{"pane": mcpString("Herdr pane ID or tmux target"), "keys": mcpString("Enter, y, n, Tab, Esc, or C-c")}, "pane", "keys"), Call: a.mcpAgentKeys},
		{Name: "agent_interrupt", Description: "Interrupt an agent pane with C-c.", InputSchema: mcpSchema(map[string]any{"pane": mcpString("Herdr pane ID or tmux target")}, "pane"), Call: a.mcpAgentInterrupt},
		{Name: "agent_focus", Description: "Focus an agent pane in herdr.", InputSchema: mcpSchema(map[string]any{"pane": mcpString("Herdr pane ID")}, "pane"), Call: a.mcpAgentFocus},
		{Name: "alerts_list", Description: "List alerts, optionally filtered by state.", InputSchema: mcpSchema(map[string]any{"state": mcpString("open, acknowledged, snoozed, or resolved")}), Call: a.mcpAlertsList},
		{Name: "alert_ack", Description: "Acknowledge one alert.", InputSchema: mcpSchema(map[string]any{"id": mcpString("Alert ID")}, "id"), Call: a.mcpAlertAction("ack")},
		{Name: "alert_snooze", Description: "Snooze one alert until an RFC3339 time or duration such as 2h.", InputSchema: mcpSchema(map[string]any{"id": mcpString("Alert ID"), "until": mcpString("RFC3339 timestamp or duration")}, "id"), Call: a.mcpAlertAction("snooze")},
		{Name: "alert_resolve", Description: "Resolve one alert.", InputSchema: mcpSchema(map[string]any{"id": mcpString("Alert ID")}, "id"), Call: a.mcpAlertAction("resolve")},
		{Name: "alert_create", Description: "Create a local alert in the box inbox.", InputSchema: mcpSchema(map[string]any{"source": mcpString("Alert source"), "severity": mcpString("info, warning, or critical"), "title": mcpString("Alert title"), "body": mcpString("Alert body")}, "source", "severity", "title"), Call: a.mcpAlertCreate},
		{Name: "usage", Description: "Read local Claude and Codex usage for a number of days.", InputSchema: mcpSchema(map[string]any{"days": mcpInteger("1 to 30 days")}), Call: a.mcpUsage},
		{Name: "usage_all", Description: "Read usage totals across this box and reachable configured boxes.", InputSchema: mcpSchema(map[string]any{"days": mcpInteger("1 to 30 days")}), Call: a.mcpUsageAll},
		{Name: "boxes", Description: "List configured boxes and their reachability.", InputSchema: empty(), Call: func(ctx context.Context, _ map[string]any) (mcpToolResult, error) {
			return mcpJSONResult(a.boxes(ctx)), nil
		}},
		{Name: "devices", Description: "List discovered Tailscale devices.", InputSchema: empty(), Call: func(ctx context.Context, _ map[string]any) (mcpToolResult, error) {
			return mcpJSONResult(a.discoveredDevices(ctx)), nil
		}},
		{Name: "apps_list", Description: "List built-in and configured apps.", InputSchema: empty(), Call: a.mcpAppsList},
		{Name: "app_start", Description: "Start a managed app.", InputSchema: mcpSchema(map[string]any{"id": mcpString("App ID")}, "id"), Call: a.mcpAppAction("start")},
		{Name: "app_stop", Description: "Stop a managed app.", InputSchema: mcpSchema(map[string]any{"id": mcpString("App ID")}, "id"), Call: a.mcpAppAction("stop")},
		{Name: "app_log", Description: "Read the tail of a managed app log.", InputSchema: mcpSchema(map[string]any{"id": mcpString("App ID"), "lines": mcpInteger("Number of log lines")}, "id"), Call: a.mcpAppLog},
		{Name: "files_list", Description: "List files in the configured files root.", InputSchema: mcpSchema(map[string]any{"path": mcpString("Path relative to the files root")}), Call: a.mcpFilesList},
		{Name: "file_read", Description: "Read a file from the configured files root.", InputSchema: mcpSchema(map[string]any{"path": mcpString("Path relative to the files root"), "max": mcpInteger("Maximum bytes")}, "path"), Call: a.mcpFileRead},
		{Name: "file_write", Description: "Atomically write an editable file with an optional If-Match mtime.", InputSchema: mcpSchema(map[string]any{"path": mcpString("Path relative to the files root"), "content": mcpString("Complete file content"), "ifMatch": mcpString("Edit mtime from file_read")}, "path", "content"), Call: a.mcpFileWrite},
		{Name: "git_repos", Description: "Discover repositories under configured roots.", InputSchema: empty(), Call: a.mcpGitRepos},
		{Name: "git_status", Description: "Read changed files for a discovered repository.", InputSchema: mcpSchema(map[string]any{"repo": mcpString("Repository path")}, "repo"), Call: a.mcpGitStatus},
		{Name: "git_diff", Description: "Read a capped diff for a discovered repository path.", InputSchema: mcpSchema(map[string]any{"repo": mcpString("Repository path"), "path": mcpString("Path inside the repository"), "staged": map[string]any{"type": "boolean"}}, "repo"), Call: a.mcpGitDiff},
		{Name: "git_log", Description: "Read recent commits for a discovered repository.", InputSchema: mcpSchema(map[string]any{"repo": mcpString("Repository path"), "n": mcpInteger("1 to 200 commits")}, "repo"), Call: a.mcpGitLog},
		{Name: "browser_start", Description: "Start the managed Chromium browser.", InputSchema: mcpSchema(map[string]any{"headless": map[string]any{"type": "boolean"}}), Call: a.mcpBrowserStart},
		{Name: "browser_stop", Description: "Stop the managed Chromium browser.", InputSchema: empty(), Call: a.mcpBrowserStop},
		{Name: "browser_pages", Description: "List pages in the managed browser.", InputSchema: empty(), Call: a.mcpBrowserPages},
		{Name: "browser_navigate", Description: "Navigate a browser page to an HTTP or HTTPS URL.", InputSchema: mcpSchema(map[string]any{"page": mcpString("Page ID"), "url": mcpString("HTTP or HTTPS URL")}, "url"), Call: a.mcpBrowserNavigate},
		{Name: "browser_screenshot", Description: "Capture a browser page as a PNG image.", InputSchema: mcpSchema(map[string]any{"page": mcpString("Page ID")}), Call: a.mcpBrowserScreenshot},
		{Name: "browser_click", Description: "Click coordinates in a browser page.", InputSchema: mcpSchema(map[string]any{"page": mcpString("Page ID"), "x": mcpNumber("X coordinate"), "y": mcpNumber("Y coordinate")}, "x", "y"), Call: a.mcpBrowserClick},
		{Name: "browser_type", Description: "Insert text into the focused browser element.", InputSchema: mcpSchema(map[string]any{"page": mcpString("Page ID"), "text": mcpString("Text to insert")}, "text"), Call: a.mcpBrowserType},
	}
	if a.cfg.AllowRun {
		tools = append(tools, mcpTool{Name: "run", Description: "Run a command as the boxdeck service account. Enabled only when allowRun is true.", InputSchema: mcpSchema(map[string]any{"cmd": mcpString("Shell command"), "cwd": mcpString("Working directory"), "timeoutMs": mcpInteger("Timeout up to 60000 milliseconds")}, "cmd"), Call: a.mcpRun})
	}
	sort.Slice(tools, func(i, j int) bool { return tools[i].Name < tools[j].Name })
	return tools
}

type mcpHTTPResult struct {
	Status int
	Header http.Header
	Body   []byte
}

func (a *app) mcpHandler(ctx context.Context, method, path string, body []byte, handler http.HandlerFunc) mcpHTTPResult {
	req, _ := http.NewRequestWithContext(ctx, method, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler(response, req)
	return mcpHTTPResult{Status: response.Code, Header: response.Header(), Body: response.Body.Bytes()}
}

func mcpDecodeHTTPResult(result mcpHTTPResult) (any, error) {
	if result.Status < 200 || result.Status >= 300 {
		var message struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(result.Body, &message) == nil && message.Error != "" {
			return nil, errors.New(message.Error)
		}
		if len(result.Body) > 0 {
			return nil, errors.New(strings.TrimSpace(string(result.Body)))
		}
		return nil, fmt.Errorf("request failed with HTTP %d", result.Status)
	}
	if len(result.Body) == 0 {
		return map[string]any{"ok": true}, nil
	}
	var value any
	if err := json.Unmarshal(result.Body, &value); err == nil {
		return value, nil
	}
	return string(result.Body), nil
}

func (a *app) mcpEndpoint(ctx context.Context, method, path string, body any, handler http.HandlerFunc) (any, error) {
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return mcpDecodeHTTPResult(a.mcpHandler(ctx, method, path, b, handler))
}

func (a *app) mcpProcesses(_ context.Context, args map[string]any) (mcpToolResult, error) {
	procs := a.collect.getProcesses()
	filter := strings.ToLower(strings.TrimSpace(mcpStringArg(args, "filter")))
	if filter != "" {
		filtered := procs[:0]
		for _, process := range procs {
			if strings.Contains(strings.ToLower(process.Args), filter) || strings.Contains(strconv.Itoa(process.PID), filter) {
				filtered = append(filtered, process)
			}
		}
		procs = filtered
	}
	sort.SliceStable(procs, func(i, j int) bool {
		switch mcpStringArg(args, "sort") {
		case "mem", "rss":
			return procs[i].RSS > procs[j].RSS
		case "age", "secs":
			return procs[i].Secs > procs[j].Secs
		case "pid":
			return procs[i].PID < procs[j].PID
		default:
			return procs[i].CPU > procs[j].CPU
		}
	})
	n := mcpIntArg(args, "n", 20)
	if n < 1 {
		n = 1
	}
	if n > 200 {
		n = 200
	}
	if len(procs) > n {
		procs = procs[:n]
	}
	result := make([]object, 0, len(procs))
	for _, process := range procs {
		result = append(result, object{"pid": process.PID, "ppid": process.PPID, "secs": process.Secs, "cpu": process.CPU, "rss": process.RSS, "args": process.Args})
	}
	return mcpJSONResult(result), nil
}

func mcpStringArg(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func mcpIntArg(args map[string]any, key string, fallback int) int {
	switch value := args[key].(type) {
	case float64:
		return int(value)
	case json.Number:
		n, _ := strconv.Atoi(string(value))
		return n
	case int:
		return value
	default:
		return fallback
	}
}

func (a *app) mcpProcessKill(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/proc/kill", object{"pid": mcpIntArg(args, "pid", 0), "signal": mcpStringArg(args, "signal"), "startTicks": mcpIntArg(args, "startTicks", 0)}, a.killProc)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAgentRead(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/herd/read?pane=" + url.QueryEscape(mcpStringArg(args, "pane")) + "&lines=" + strconv.Itoa(mcpIntArg(args, "lines", 80))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.herdRead)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAgentPrompt(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/herd/prompt", object{"pane": mcpStringArg(args, "pane"), "text": mcpStringArg(args, "text")}, a.herdPrompt)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAgentKeys(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/herd/keys", object{"pane": mcpStringArg(args, "pane"), "keys": mcpStringArg(args, "keys")}, a.herdKeys)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAgentInterrupt(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/herd/interrupt", object{"pane": mcpStringArg(args, "pane")}, a.herdInterrupt)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAgentFocus(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/herdr/focus", object{"pane_id": mcpStringArg(args, "pane")}, a.focusHerdr)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAlertsList(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/alerts?state=" + url.QueryEscape(mcpStringArg(args, "state"))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.alertsAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAlertAction(action string) func(context.Context, map[string]any) (mcpToolResult, error) {
	return func(ctx context.Context, args map[string]any) (mcpToolResult, error) {
		id := url.PathEscape(mcpStringArg(args, "id"))
		body := any(nil)
		if action == "snooze" {
			body = object{"until": mcpStringArg(args, "until")}
		}
		value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/alerts/"+id+"/"+action, body, a.alertActionAPI)
		if err != nil {
			return mcpToolResult{}, err
		}
		return mcpJSONResult(value), nil
	}
}

func (a *app) mcpAlertCreate(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/alerts", object{"source": mcpStringArg(args, "source"), "severity": mcpStringArg(args, "severity"), "title": mcpStringArg(args, "title"), "body": mcpStringArg(args, "body")}, a.alertsAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpUsage(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/usage?days=" + strconv.Itoa(mcpIntArg(args, "days", 30))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.usageAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpUsageAll(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/usage/all?days=" + strconv.Itoa(mcpIntArg(args, "days", 30))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.usageAllAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAppsList(ctx context.Context, _ map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodGet, "/api/apps", nil, a.appsAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpAppAction(action string) func(context.Context, map[string]any) (mcpToolResult, error) {
	return func(ctx context.Context, args map[string]any) (mcpToolResult, error) {
		id := url.PathEscape(mcpStringArg(args, "id"))
		value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/apps/"+id+"/"+action, nil, a.appActionAPI)
		if err != nil {
			return mcpToolResult{}, err
		}
		return mcpJSONResult(value), nil
	}
}

func (a *app) mcpAppLog(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/apps/" + url.PathEscape(mcpStringArg(args, "id")) + "/log?lines=" + strconv.Itoa(mcpIntArg(args, "lines", 20))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.appActionAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpFilesList(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/files?path=" + url.QueryEscape(mcpStringArg(args, "path"))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.apiFiles)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpFileRead(_ context.Context, args map[string]any) (mcpToolResult, error) {
	relative := strings.TrimPrefix(mcpStringArg(args, "path"), "/")
	abs, err := filePath(a.cfg.FilesRoot, relative)
	if err != nil {
		return mcpToolResult{}, errors.New("path is outside the files root")
	}
	info, err := os.Stat(abs)
	if err != nil {
		return mcpToolResult{}, errors.New("file not found")
	}
	if !info.Mode().IsRegular() {
		return mcpToolResult{}, errors.New("only regular files can be read")
	}
	max := mcpIntArg(args, "max", 1<<20)
	if max < 1 {
		max = 1
	}
	if max > 8<<20 {
		max = 8 << 20
	}
	f, err := os.Open(abs)
	if err != nil {
		return mcpToolResult{}, errors.New("file not found")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, int64(max)+1))
	if err != nil {
		return mcpToolResult{}, errors.New("cannot read file")
	}
	truncated := len(b) > max
	if truncated {
		b = b[:max]
	}
	value := object{"path": "/" + relative, "bytes": len(b), "truncated": truncated, "mtime": editMtime(info)}
	if strings.IndexByte(string(b), 0) >= 0 {
		value["encoding"] = "base64"
		value["content"] = base64.StdEncoding.EncodeToString(b)
	} else {
		value["encoding"] = "utf-8"
		value["content"] = string(b)
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpFileWrite(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/edit?path=" + url.QueryEscape(mcpStringArg(args, "path"))
	request, _ := http.NewRequestWithContext(ctx, http.MethodPut, path, strings.NewReader(mcpStringArg(args, "content")))
	if match := strings.TrimSpace(mcpStringArg(args, "ifMatch")); match != "" {
		request.Header.Set("If-Match", match)
	}
	recorder := httptest.NewRecorder()
	a.edit(recorder, request)
	result := mcpHTTPResult{Status: recorder.Code, Header: recorder.Header(), Body: recorder.Body.Bytes()}
	value, err := mcpDecodeHTTPResult(result)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpGitRepos(ctx context.Context, _ map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodGet, "/api/git/repos", nil, a.gitAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpGitStatus(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodGet, "/api/git/status?repo="+url.QueryEscape(mcpStringArg(args, "repo")), nil, a.gitAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpGitDiff(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/git/diff?repo=" + url.QueryEscape(mcpStringArg(args, "repo")) + "&path=" + url.QueryEscape(mcpStringArg(args, "path"))
	if staged, ok := args["staged"].(bool); ok && staged {
		path += "&staged=true"
	}
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.gitAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpGitLog(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	path := "/api/git/log?repo=" + url.QueryEscape(mcpStringArg(args, "repo")) + "&n=" + strconv.Itoa(mcpIntArg(args, "n", 60))
	value, err := a.mcpEndpoint(ctx, http.MethodGet, path, nil, a.gitAPI)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpBrowserStart(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/browser/start", args, a.browserStart)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpBrowserStop(ctx context.Context, _ map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/browser/stop", nil, a.browserStop)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpBrowserPages(ctx context.Context, _ map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodGet, "/api/browser", nil, a.browserStatus)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func (a *app) mcpBrowserPage(args map[string]any) (browserPage, error) {
	state := a.browser.status()
	if !state.Running {
		return browserPage{}, errors.New("Start the browser first")
	}
	page, ok := browserPageFor(state.Pages, mcpStringArg(args, "page"))
	if !ok || page.WebSocketDebuggerURL == "" {
		return browserPage{}, errors.New("No browser page is open")
	}
	return page, nil
}

func (a *app) mcpBrowserNavigate(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	target, err := url.Parse(strings.TrimSpace(mcpStringArg(args, "url")))
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Host == "" || target.User != nil {
		return mcpToolResult{}, errors.New("url must be an http(s) URL")
	}
	page, err := a.mcpBrowserPage(args)
	if err != nil {
		return mcpToolResult{}, err
	}
	value, err := mcpCDPCall(ctx, page.WebSocketDebuggerURL, "Page.navigate", object{"url": target.String()})
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(object{"page": page.ID, "result": value}), nil
}

func (a *app) mcpBrowserScreenshot(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	page, err := a.mcpBrowserPage(args)
	if err != nil {
		return mcpToolResult{}, err
	}
	data, err := captureCDPScreenshot(ctx, page.WebSocketDebuggerURL)
	if err != nil {
		return mcpToolResult{}, errors.New("Could not capture this page")
	}
	return mcpToolResult{ResultType: "complete", Content: []mcpToolContent{{Type: "image", Data: base64.StdEncoding.EncodeToString(data), MimeType: "image/png"}}, Structured: object{"page": page.ID, "mimeType": "image/png"}}, nil
}

func (a *app) mcpBrowserClick(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	page, err := a.mcpBrowserPage(args)
	if err != nil {
		return mcpToolResult{}, err
	}
	x, y := args["x"], args["y"]
	for _, event := range []string{"mousePressed", "mouseReleased"} {
		if _, err := mcpCDPCall(ctx, page.WebSocketDebuggerURL, "Input.dispatchMouseEvent", object{"type": event, "x": x, "y": y, "button": "left", "clickCount": 1}); err != nil {
			return mcpToolResult{}, err
		}
	}
	return mcpJSONResult(object{"ok": true, "page": page.ID}), nil
}

func (a *app) mcpBrowserType(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	page, err := a.mcpBrowserPage(args)
	if err != nil {
		return mcpToolResult{}, err
	}
	if len(mcpStringArg(args, "text")) > 64<<10 {
		return mcpToolResult{}, errors.New("text is too long")
	}
	if _, err := mcpCDPCall(ctx, page.WebSocketDebuggerURL, "Input.insertText", object{"text": mcpStringArg(args, "text")}); err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(object{"ok": true, "page": page.ID}), nil
}

func (a *app) mcpRun(ctx context.Context, args map[string]any) (mcpToolResult, error) {
	value, err := a.mcpEndpoint(ctx, http.MethodPost, "/api/run", args, a.apiRun)
	if err != nil {
		return mcpToolResult{}, err
	}
	return mcpJSONResult(value), nil
}

func mcpCDPCall(ctx context.Context, wsURL, method string, params any) (any, error) {
	conn, reader, response, err := dialCDPWebsocket(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if response != nil && response.Body != nil {
		_ = response.Body.Close()
	}
	_ = conn.SetDeadline(time.Now().Add(6 * time.Second))
	message, _ := json.Marshal(object{"id": 1, "method": method, "params": params})
	if err := writeWebSocketFrame(conn, message); err != nil {
		return nil, err
	}
	for {
		opcode, payload, err := readWebSocketFrame(reader)
		if err != nil {
			return nil, err
		}
		if opcode == 0x9 {
			if err := writeWebSocketControl(conn, 0xA, payload); err != nil {
				return nil, err
			}
			continue
		}
		if opcode != 0x1 {
			continue
		}
		var result struct {
			ID     int             `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  json.RawMessage `json:"error"`
		}
		if json.Unmarshal(payload, &result) != nil || result.ID != 1 {
			continue
		}
		if len(result.Error) > 0 && string(result.Error) != "null" {
			return nil, errors.New("browser action failed")
		}
		var value any
		if len(result.Result) > 0 {
			_ = json.Unmarshal(result.Result, &value)
		}
		return value, nil
	}
}

func mcpBridge(in io.Reader, out io.Writer, target, token string) error {
	target = strings.TrimRight(strings.TrimSpace(target), "/")
	if target == "" {
		return errors.New("--to is required")
	}
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 4096), 2<<20)
	client := &http.Client{Timeout: 70 * time.Second}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var request mcpRPCRequest
		if err := json.Unmarshal([]byte(line), &request); err != nil {
			return fmt.Errorf("invalid JSON-RPC input: %w", err)
		}
		params, _ := mcpParams(request.Params)
		version, _ := mcpBodyVersion(request, params)
		if version == "" {
			version = mcpCurrentVersion
		}
		body := []byte(line)
		if version == mcpCurrentVersion {
			if _, metaVersion := mcpMeta(params); metaVersion == "" {
				if params == nil {
					params = map[string]json.RawMessage{}
				}
				params["_meta"] = json.RawMessage(`{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"boxdeck-stdio","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`)
				request.Params, _ = json.Marshal(params)
				request.JSONRPC = "2.0"
				body, _ = json.Marshal(request)
			}
		}
		requestURL, err := url.Parse(target)
		if err != nil {
			return err
		}
		if requestURL.Path == "" {
			requestURL.Path = "/mcp"
		}
		req, err := http.NewRequest(http.MethodPost, requestURL.String(), strings.NewReader(string(body)))
		if err != nil {
			return err
		}
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("MCP-Protocol-Version", version)
		req.Header.Set("Mcp-Method", request.Method)
		if request.Method == "tools/call" {
			var call struct {
				Name string `json:"name"`
			}
			_ = json.Unmarshal(request.Params, &call)
			if call.Name != "" {
				req.Header.Set("Mcp-Name", call.Name)
			}
		}
		response, err := client.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		_ = response.Body.Close()
		if readErr != nil {
			return readErr
		}
		if response.StatusCode < 200 || response.StatusCode >= 300 {
			return fmt.Errorf("MCP server returned HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(data)))
		}
		if len(data) == 0 {
			continue
		}
		if strings.Contains(response.Header.Get("Content-Type"), "text/event-stream") {
			for _, eventLine := range strings.Split(string(data), "\n") {
				if strings.HasPrefix(eventLine, "data:") {
					data = []byte(strings.TrimSpace(strings.TrimPrefix(eventLine, "data:")))
					break
				}
			}
		}
		if _, err := out.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func mcpCommand(args []string) error {
	flags := flag.NewFlagSet("boxdeck mcp", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	target := flags.String("to", os.Getenv("BOXDECK_TO"), "MCP endpoint")
	token := flags.String("token", os.Getenv("BOXDECK_TOKEN"), "Bearer token")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*token) == "" {
		return errors.New("--token is required")
	}
	return mcpBridge(os.Stdin, os.Stdout, *target, *token)
}
