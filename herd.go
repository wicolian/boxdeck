package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var ansiSequenceRE = regexp.MustCompile(`\x1b(?:\[[0-?]*[ -/]*[@-~]|\][^\x07]*(?:\x07|\x1b\\))`)
var panePromptRE = regexp.MustCompile(`(?i)(\(\s*y\s*/\s*n\s*\)|press\s+(?:enter|return)|\btrust\b)`)

func stripANSI(value string) string {
	return ansiSequenceRE.ReplaceAllString(value, "")
}

func tailPaneText(raw string, lines int) string {
	clean := strings.ReplaceAll(stripANSI(raw), "\r", "")
	clean = strings.TrimRight(clean, "\n")
	if clean == "" {
		return ""
	}
	parts := strings.Split(clean, "\n")
	if lines < 1 {
		lines = 80
	}
	if lines > 400 {
		lines = 400
	}
	if len(parts) > lines {
		parts = parts[len(parts)-lines:]
	}
	return strings.Join(parts, "\n")
}

func paneNeedsInput(text string) bool { return panePromptRE.MatchString(text) }

func validPaneName(pane string) bool {
	if strings.TrimSpace(pane) == "" || len(pane) > 256 {
		return false
	}
	for _, r := range pane {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func paneLineCount(raw string) int {
	if raw == "" {
		return 0
	}
	return strings.Count(raw, "\n") + 1
}

func commandWithTimeout(ctx context.Context, timeout time.Duration, name string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, name, args...)
	cmd.WaitDelay = time.Second
	b, err := cmd.Output()
	if err != nil {
		if commandCtx.Err() != nil {
			return nil, commandCtx.Err()
		}
		return nil, err
	}
	return b, nil
}

func readPane(ctx context.Context, home, pane string) (string, string, error) {
	if binary := herdrBinary(home); binary != "" {
		if _, err := os.Stat(herdrSocket(home)); err == nil {
			if output, err := commandWithTimeout(ctx, 5*time.Second, binary, "agent", "read", pane); err == nil {
				return tailPaneText(string(output), 400), "herdr", nil
			}
		}
	}
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return "", "", fmt.Errorf("neither herdr nor tmux is available")
	}
	output, err := commandWithTimeout(ctx, 5*time.Second, tmux, "capture-pane", "-p", "-t", pane, "-S", "-400")
	if err != nil {
		return "", "", fmt.Errorf("could not read pane")
	}
	return tailPaneText(string(output), 400), "tmux", nil
}

func normalizedKey(value string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "enter", "return":
		return "Enter", true
	case "y":
		return "y", true
	case "n":
		return "n", true
	case "tab":
		return "Tab", true
	case "esc", "escape":
		return "Escape", true
	case "c-c", "ctrl-c", "control-c":
		return "C-c", true
	default:
		return "", false
	}
}

func sendPaneKeys(ctx context.Context, home, pane, keys string) error {
	key, ok := normalizedKey(keys)
	if !ok {
		return fmt.Errorf("keys must be Enter, y, n, Tab or Esc")
	}
	if binary := herdrBinary(home); binary != "" {
		if _, err := os.Stat(herdrSocket(home)); err == nil {
			if _, err := commandWithTimeout(ctx, 5*time.Second, binary, "agent", "send-keys", pane, key); err == nil {
				return nil
			}
		}
	}
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("neither herdr nor tmux is available")
	}
	_, err = commandWithTimeout(ctx, 5*time.Second, tmux, "send-keys", "-t", pane, key)
	if err != nil {
		return fmt.Errorf("could not send keys")
	}
	return nil
}

func sendPanePrompt(ctx context.Context, home, pane, prompt string) error {
	if strings.TrimSpace(prompt) == "" || len(prompt) > 64<<10 {
		return fmt.Errorf("text is required")
	}
	if binary := herdrBinary(home); binary != "" {
		if _, err := os.Stat(herdrSocket(home)); err == nil {
			if _, err := commandWithTimeout(ctx, 5*time.Second, binary, "agent", "prompt", pane, prompt); err == nil {
				return nil
			}
		}
	}
	tmux, err := exec.LookPath("tmux")
	if err != nil {
		return fmt.Errorf("neither herdr nor tmux is available")
	}
	commandCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, tmux, "send-keys", "-t", pane, "-l", prompt)
	cmd.WaitDelay = time.Second
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("could not send prompt")
	}
	return nil
}

func (a *app) herdRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read a pane"})
		return
	}
	pane := strings.TrimSpace(r.URL.Query().Get("pane"))
	if !validPaneName(pane) {
		jsonReply(w, http.StatusBadRequest, object{"error": "pane is required"})
		return
	}
	lines := 80
	if raw := r.URL.Query().Get("lines"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 400 {
			jsonReply(w, http.StatusBadRequest, object{"error": "lines must be between 1 and 400"})
			return
		}
		lines = parsed
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	text, source, err := readPane(ctx, a.cfg.home, pane)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not read this pane"})
		return
	}
	text = tailPaneText(text, lines)
	jsonReply(w, http.StatusOK, object{"pane": pane, "text": text, "lines": paneLineCount(text), "source": source, "needsYou": paneNeedsInput(text)})
}

func (a *app) herdInterrupt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to interrupt a pane"})
		return
	}
	var body struct {
		Pane string `json:"pane"`
	}
	if !decodeJSONBody(w, r, &body, 4096) {
		return
	}
	if !validPaneName(body.Pane) {
		jsonReply(w, http.StatusBadRequest, object{"error": "pane is required"})
		return
	}
	if err := sendPaneKeys(r.Context(), a.cfg.home, body.Pane, "C-c"); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not interrupt this pane"})
		return
	}
	jsonReply(w, http.StatusOK, object{"ok": true, "pane": body.Pane, "keys": "C-c"})
}

func (a *app) herdKeys(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to send keys"})
		return
	}
	var body struct {
		Pane string `json:"pane"`
		Keys string `json:"keys"`
	}
	if !decodeJSONBody(w, r, &body, 4096) {
		return
	}
	if !validPaneName(body.Pane) || strings.TrimSpace(body.Keys) == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "pane and keys are required"})
		return
	}
	if _, ok := normalizedKey(body.Keys); !ok {
		jsonReply(w, http.StatusBadRequest, object{"error": "keys must be Enter, y, n, Tab or Esc"})
		return
	}
	if err := sendPaneKeys(r.Context(), a.cfg.home, body.Pane, body.Keys); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not send keys to this pane"})
		return
	}
	jsonReply(w, http.StatusOK, object{"ok": true, "pane": body.Pane, "keys": body.Keys})
}

func (a *app) herdPrompt(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to send a prompt"})
		return
	}
	var body struct {
		Pane string `json:"pane"`
		Text string `json:"text"`
	}
	if !decodeJSONBody(w, r, &body, 64<<10) {
		return
	}
	if !validPaneName(body.Pane) || strings.TrimSpace(body.Text) == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "pane and text are required"})
		return
	}
	if err := sendPanePrompt(r.Context(), a.cfg.home, body.Pane, body.Text); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not send a prompt to this pane"})
		return
	}
	jsonReply(w, http.StatusOK, object{"ok": true, "pane": body.Pane})
}
