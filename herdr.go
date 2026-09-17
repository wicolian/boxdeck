package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

type herdrAgent struct {
	Agent       string            `json:"agent"`
	Status      string            `json:"agent_status"`
	CWD         string            `json:"cwd"`
	PaneID      string            `json:"pane_id"`
	TabID       string            `json:"tab_id"`
	WorkspaceID string            `json:"workspace_id"`
	Title       string            `json:"terminal_title_stripped"`
	Tokens      map[string]string `json:"tokens,omitempty"`
}
type herdrWorkspace struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Tabs int    `json:"tabs"`
}
type herdrState struct {
	Running    bool             `json:"running"`
	Workspaces []herdrWorkspace `json:"workspaces"`
	Agents     []herdrAgent     `json:"agents"`
}

func emptyHerdr() herdrState {
	return herdrState{Workspaces: []herdrWorkspace{}, Agents: []herdrAgent{}}
}
func parseHerdr(b []byte) (herdrState, error) {
	var response struct {
		Error  json.RawMessage `json:"error"`
		Result struct {
			Type     string `json:"type"`
			Snapshot struct {
				Agents     []herdrAgent `json:"agents"`
				Workspaces []struct {
					ID    string `json:"workspace_id"`
					Label string `json:"label"`
					Tabs  int    `json:"tab_count"`
				} `json:"workspaces"`
			} `json:"snapshot"`
		} `json:"result"`
	}
	h := emptyHerdr()
	if err := json.Unmarshal(b, &response); err != nil {
		return h, err
	}
	if response.Result.Type != "session_snapshot" || len(response.Error) > 0 {
		return h, fmt.Errorf("herdr snapshot unavailable")
	}
	h.Running = true
	if response.Result.Snapshot.Agents != nil {
		h.Agents = response.Result.Snapshot.Agents
	}
	for _, w := range response.Result.Snapshot.Workspaces {
		h.Workspaces = append(h.Workspaces, herdrWorkspace{w.ID, w.Label, w.Tabs})
	}
	return h, nil
}
func herdrBinary(home string) string {
	if p, err := exec.LookPath("herdr"); err == nil {
		return p
	}
	p := filepath.Join(home, ".local/bin/herdr")
	if st, err := os.Stat(p); err == nil && st.Mode()&0111 != 0 {
		return p
	}
	return ""
}
func herdrSocket(home string) string { return filepath.Join(home, ".config/herdr/herdr.sock") }

// Herdr's public API: one JSON request and response per line. Read only the
// needed schema; additive fields and protocol version bumps remain compatible.
func herdrRequest(ctx context.Context, socket, method string, params any) ([]byte, error) {
	d := net.Dialer{Timeout: time.Second}
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err = json.NewEncoder(conn).Encode(object{"id": "boxdeck", "method": method, "params": params}); err != nil {
		return nil, err
	}
	b, err := bufio.NewReader(io.LimitReader(conn, 8<<20)).ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return b, nil
}
func (c *collectors) getHerdr() herdrState {
	return c.herdr.get(3*time.Second, func() herdrState {
		if herdrBinary(c.cfg.home) == "" {
			return emptyHerdr()
		}
		if _, err := os.Stat(herdrSocket(c.cfg.home)); err != nil {
			return emptyHerdr()
		}
		b, err := herdrRequest(c.ctx, herdrSocket(c.cfg.home), "session.snapshot", object{})
		if err != nil {
			return emptyHerdr()
		}
		h, err := parseHerdr(b)
		if err != nil {
			return emptyHerdr()
		}
		return h
	})
}
