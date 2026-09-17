package main

import (
	"context"
	"encoding/json"
	"io"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (a *app) api(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/boxes":
		a.apiBoxes(w, r)
	case "/api/ports":
		a.apiPorts(w, r)
	case "/api/procs":
		a.apiProcs(w, r)
	case "/api/agents":
		a.apiAgents(w, r)
	case "/api/tmux":
		a.apiTmux(w, r)
	case "/api/docker":
		a.apiDocker(w, r)
	case "/api/files":
		a.apiFiles(w, r)
	case "/api/herdr/prompt":
		a.apiHerdrPrompt(w, r)
	case "/api/run":
		a.apiRun(w, r)
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "API route not found"})
	}
}

func requireMethod(w http.ResponseWriter, r *http.Request, method string) bool {
	if r.Method == method {
		return true
	}
	w.Header().Set("Allow", method)
	jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use " + method + " for this endpoint"})
	return false
}

func (a *app) apiBoxes(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jsonReply(w, http.StatusOK, a.boxes(r.Context()))
}

func (a *app) apiPorts(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jsonReply(w, http.StatusOK, a.collect.getPorts())
}

func (a *app) apiProcs(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	n := 20
	if raw := r.URL.Query().Get("n"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			jsonReply(w, http.StatusBadRequest, object{"error": "n must be between 1 and 200"})
			return
		}
		n = parsed
	}
	procs := append([]process(nil), a.collect.getProcesses()...)
	sortMode := r.URL.Query().Get("sort")
	sort.SliceStable(procs, func(i, j int) bool {
		switch sortMode {
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
	if len(procs) > n {
		procs = procs[:n]
	}
	result := make([]object, 0, len(procs))
	for _, p := range procs {
		result = append(result, object{"pid": p.PID, "ppid": p.PPID, "secs": p.Secs, "cpu": p.CPU, "rss": p.RSS, "args": p.Args})
	}
	jsonReply(w, http.StatusOK, result)
}

func (a *app) apiAgents(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	procs := a.collect.getProcesses()
	panes := a.collect.getPanes()
	herdr := a.collect.getHerdr()
	jsonReply(w, http.StatusOK, mergeAgents(procs, panes, herdr, a.cfg, paneFromEnvironment))
}

func (a *app) apiTmux(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jsonReply(w, http.StatusOK, tmuxSessions(a.collect.getPanes()))
}

func (a *app) apiDocker(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	jsonReply(w, http.StatusOK, a.collect.getDocker())
}

type apiFileEntry struct {
	Name    string `json:"name"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"modTime"`
	Status  string `json:"status,omitempty"`
}

func (a *app) apiFiles(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	relative := strings.TrimPrefix(r.URL.Query().Get("path"), "/")
	abs, err := filePath(a.cfg.FilesRoot, relative)
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	if st.IsDir() {
		entries, err := os.ReadDir(abs)
		if err != nil {
			jsonReply(w, http.StatusForbidden, object{"error": "cannot read this folder"})
			return
		}
		result := make([]apiFileEntry, 0, len(entries))
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), ".") || entry.Name() == "node_modules" {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			result = append(result, apiFileEntry{Name: entry.Name(), Dir: entry.IsDir(), Size: info.Size(), ModTime: info.ModTime().UTC().Format(time.RFC3339)})
		}
		if repo, repoErr := gitRepoAt(abs); repoErr == nil {
			status, statusErr := a.cachedGitStatus(repo)
			if statusErr == nil {
				marks := map[string]string{}
				for _, item := range status {
					mark := item.Status
					if mark == "" {
						mark = "?"
					}
					marks[filepath.ToSlash(item.Path)] = mark
				}
				for i := range result {
					rel, relErr := filepath.Rel(repo, filepath.Join(abs, result[i].Name))
					if relErr == nil {
						result[i].Status = marks[filepath.ToSlash(rel)]
					}
				}
			}
		}
		sort.Slice(result, func(i, j int) bool {
			if result[i].Dir != result[j].Dir {
				return result[i].Dir
			}
			return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
		})
		jsonReply(w, http.StatusOK, object{"path": "/" + relative, "entries": result})
		return
	}
	if !st.Mode().IsRegular() {
		jsonReply(w, http.StatusForbidden, object{"error": "only regular files can be read"})
		return
	}
	if st.Size() > 8<<20 {
		jsonReply(w, http.StatusRequestEntityTooLarge, object{"error": "file is too large"})
		return
	}
	contentType := mime.TypeByExtension(strings.ToLower(filepath.Ext(abs)))
	switch strings.ToLower(filepath.Ext(abs)) {
	case ".md", ".markdown":
		contentType = "text/markdown; charset=utf-8"
	case ".txt", ".log", ".csv":
		contentType = "text/plain; charset=utf-8"
	case ".js":
		contentType = "text/javascript; charset=utf-8"
	}
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	// The API is a data channel for agents, never a place to render a page: active
	// content is sent as a download inside a sandbox so a hostile file in the home
	// folder cannot run with the deck's cookie.
	w.Header().Set("Content-Security-Policy", "sandbox; default-src 'none'")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+strings.ReplaceAll(filepath.Base(abs), "\"", "")+"\"")
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.FormatInt(st.Size(), 10))
	_, _ = io.CopyN(w, f, st.Size())
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(dst); err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": "invalid JSON body"})
		return false
	}
	return true
}

func (a *app) apiHerdrPrompt(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		PaneID string `json:"pane_id"`
		Text   string `json:"text"`
	}
	if !decodeJSONBody(w, r, &body, 64<<10) {
		return
	}
	if strings.TrimSpace(body.PaneID) == "" || strings.TrimSpace(body.Text) == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "pane_id and text are required"})
		return
	}
	h := a.collect.getHerdr()
	if !h.Running {
		jsonReply(w, http.StatusNotFound, object{"error": "Herdr is not running. Start herdr on the box"})
		return
	}
	found := false
	for _, agent := range h.Agents {
		if agent.PaneID == body.PaneID {
			found = true
			break
		}
	}
	if !found {
		jsonReply(w, http.StatusNotFound, object{"error": "This pane has closed. Refresh and choose an agent"})
		return
	}
	binary := herdrBinary(a.cfg.home)
	if binary == "" {
		jsonReply(w, http.StatusNotFound, object{"error": "Herdr is not installed on the box"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, binary, "agent", "prompt", body.PaneID, body.Text)
	cmd.WaitDelay = time.Second
	stdout := &boundedOutput{limit: 1 << 20}
	stderr := &boundedOutput{limit: 1 << 20}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	if err := cmd.Run(); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": "Could not send prompt to this pane"})
		return
	}
	jsonReply(w, http.StatusOK, object{"ok": true, "pane_id": body.PaneID, "stdout": stdout.String()})
}

type runRequest struct {
	Cmd       string `json:"cmd"`
	CWD       string `json:"cwd"`
	TimeoutMs int    `json:"timeoutMs"`
}

func (a *app) apiRun(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	if !a.cfg.AllowRun {
		jsonReply(w, http.StatusForbidden, object{"error": "Remote run is disabled; set allowRun:true in config"})
		return
	}
	var body runRequest
	if !decodeJSONBody(w, r, &body, 64<<10) {
		return
	}
	if strings.TrimSpace(body.Cmd) == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "cmd is required"})
		return
	}
	timeout := 60 * time.Second
	if body.TimeoutMs > 0 && body.TimeoutMs < 60000 {
		timeout = time.Duration(body.TimeoutMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(r.Context(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "bash", "-lc", body.Cmd)
	if body.CWD != "" {
		cmd.Dir = expandHome(body.CWD, a.cfg.home)
	}
	stdout := &boundedOutput{limit: 8 << 20}
	stderr := &boundedOutput{limit: 8 << 20}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		}
		if ctx.Err() != nil {
			stderr.Write([]byte("command timed out\n"))
		}
	}
	jsonReply(w, http.StatusOK, object{"stdout": stdout.String(), "stderr": stderr.String(), "exitCode": exitCode})
}
