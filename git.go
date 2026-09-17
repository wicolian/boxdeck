package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

const gitCommandTimeout = 10 * time.Second
const gitDiffLimit = 500 << 10

type gitCommit struct {
	SHA     string `json:"sha"`
	Author  string `json:"author"`
	When    string `json:"when"`
	Subject string `json:"subject"`
}

type gitWorktree struct {
	Path      string `json:"path"`
	FilesPath string `json:"filesPath,omitempty"`
	Branch    string `json:"branch"`
	Dirty     int    `json:"dirty"`
}

type gitRepoInfo struct {
	Path       string        `json:"path"`
	Branch     string        `json:"branch"`
	Ahead      int           `json:"ahead"`
	Behind     int           `json:"behind"`
	Dirty      int           `json:"dirty"`
	LastCommit gitCommit     `json:"lastCommit"`
	Worktrees  []gitWorktree `json:"worktrees"`
}

type gitStatusCache struct {
	At      time.Time
	Entries []gitStatusEntry
}

type gitStatusEntry struct {
	Path     string `json:"path"`
	OrigPath string `json:"origPath,omitempty"`
	Index    string `json:"index"`
	Worktree string `json:"worktree"`
	Status   string `json:"status"`
	Staged   bool   `json:"staged"`
	Unstaged bool   `json:"unstaged"`
}

type gitLogRow struct {
	SHA     string   `json:"sha"`
	Parents []string `json:"parents"`
	Refs    []string `json:"refs"`
	Author  string   `json:"author"`
	When    string   `json:"when"`
	Subject string   `json:"subject"`
	Graph   string   `json:"graph"`
}

func gitCommand(ctx context.Context, repo string, args ...string) ([]byte, error) {
	commandCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(commandCtx, "git", args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if commandCtx.Err() != nil {
		return nil, fmt.Errorf("git command timed out")
	}
	if err != nil {
		message := strings.TrimSpace(string(out))
		if message == "" {
			message = err.Error()
		}
		return nil, fmt.Errorf("git: %s", message)
	}
	return out, nil
}

func pathWithin(root, target string) bool {
	rel, err := filepath.Rel(root, target)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

func (a *app) gitRoots() []string {
	roots := append([]string{}, a.cfg.RepoRoots...)
	roots = append(roots, a.cfg.ReportRoots...)
	seen := map[string]bool{}
	result := make([]string, 0, len(roots))
	for _, root := range roots {
		if root == "" {
			continue
		}
		abs, err := filepath.Abs(expandHome(root, a.cfg.home))
		if err != nil {
			continue
		}
		abs = filepath.Clean(abs)
		if !seen[abs] {
			seen[abs] = true
			result = append(result, abs)
		}
	}
	return result
}

func (a *app) validateGitRepo(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return "", fmt.Errorf("repo is required")
	}
	repo, err := filepath.Abs(expandHome(raw, a.cfg.home))
	if err != nil {
		return "", err
	}
	repo = filepath.Clean(repo)
	allowed := false
	for _, root := range a.gitRoots() {
		if pathWithin(root, repo) {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", fmt.Errorf("repo is outside configured repo roots")
	}
	info, err := os.Stat(repo)
	if err != nil || !info.IsDir() {
		return "", fmt.Errorf("repo not found")
	}
	top, err := gitCommand(context.Background(), repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", fmt.Errorf("not a git repo")
	}
	topPath, err := filepath.Abs(strings.TrimSpace(string(top)))
	if err != nil || filepath.Clean(topPath) != repo {
		return "", fmt.Errorf("repo must be the worktree root")
	}
	return repo, nil
}

func (a *app) discoverGitRepos() []gitRepoInfo {
	var paths []string
	seen := map[string]bool{}
	for _, root := range a.gitRoots() {
		info, err := os.Stat(root)
		if err != nil || !info.IsDir() {
			continue
		}
		_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !entry.IsDir() {
				return nil
			}
			if path != root {
				rel, relErr := filepath.Rel(root, path)
				if relErr != nil {
					return filepath.SkipDir
				}
				depth := len(strings.Split(filepath.ToSlash(rel), "/"))
				if depth > 3 || entry.Name() == "node_modules" || entry.Name() == ".worktrees" || strings.HasPrefix(entry.Name(), ".") {
					return filepath.SkipDir
				}
			}
			if _, statErr := os.Stat(filepath.Join(path, ".git")); statErr == nil {
				if !seen[path] {
					seen[path] = true
					paths = append(paths, path)
				}
				return filepath.SkipDir
			}
			return nil
		})
	}
	sort.Strings(paths)
	result := make([]gitRepoInfo, 0, len(paths))
	for _, path := range paths {
		if repo, err := a.gitRepoInfo(path); err == nil {
			result = append(result, repo)
		}
	}
	return result
}

func (a *app) gitRepoInfo(repo string) (gitRepoInfo, error) {
	branchBytes, err := gitCommand(context.Background(), repo, "symbolic-ref", "--short", "-q", "HEAD")
	branch := strings.TrimSpace(string(branchBytes))
	if err != nil || branch == "" {
		if head, headErr := gitCommand(context.Background(), repo, "rev-parse", "--short", "HEAD"); headErr == nil {
			branch = "(detached at " + strings.TrimSpace(string(head)) + ")"
		} else {
			branch = "(unborn)"
		}
	}
	status, err := a.cachedGitStatus(repo)
	if err != nil {
		return gitRepoInfo{}, err
	}
	result := gitRepoInfo{Path: repo, Branch: branch, Dirty: len(status), Worktrees: a.gitWorktrees(repo)}
	if counts, countErr := gitCommand(context.Background(), repo, "rev-list", "--left-right", "--count", "HEAD...@{upstream}"); countErr == nil {
		parts := strings.Fields(string(counts))
		if len(parts) == 2 {
			result.Ahead, _ = strconv.Atoi(parts[0])
			result.Behind, _ = strconv.Atoi(parts[1])
		}
	}
	if last, lastErr := gitCommand(context.Background(), repo, "log", "-1", "--format=%H%x00%an%x00%aI%x00%s"); lastErr == nil {
		fields := strings.SplitN(strings.TrimSuffix(string(last), "\n"), "\x00", 4)
		if len(fields) == 4 {
			result.LastCommit = gitCommit{SHA: fields[0], Author: fields[1], When: fields[2], Subject: fields[3]}
		}
	}
	return result, nil
}

func gitStatus(repo string) ([]gitStatusEntry, error) {
	out, err := gitCommand(context.Background(), repo, "status", "--porcelain=v2", "--untracked-files=all")
	if err != nil {
		return nil, err
	}
	return parseGitStatus(string(out)), nil
}

func gitRepoAt(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	dir := path
	if !info.IsDir() {
		dir = filepath.Dir(path)
	}
	out, err := gitCommand(context.Background(), dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	return filepath.Abs(strings.TrimSpace(string(out)))
}

func (a *app) cachedGitStatus(repo string) ([]gitStatusEntry, error) {
	a.gitStatusMu.Lock()
	defer a.gitStatusMu.Unlock()
	if a.gitStatusMemo == nil {
		a.gitStatusMemo = map[string]gitStatusCache{}
	}
	if cached, ok := a.gitStatusMemo[repo]; ok && time.Since(cached.At) < 5*time.Second {
		return append([]gitStatusEntry(nil), cached.Entries...), nil
	}
	entries, err := gitStatus(repo)
	if err != nil {
		return nil, err
	}
	a.gitStatusMemo[repo] = gitStatusCache{At: time.Now(), Entries: append([]gitStatusEntry(nil), entries...)}
	return entries, nil
}

func (a *app) invalidateGitStatus(repo string) {
	a.gitStatusMu.Lock()
	defer a.gitStatusMu.Unlock()
	delete(a.gitStatusMemo, repo)
}

func parseGitStatus(input string) []gitStatusEntry {
	entries := []gitStatusEntry{}
	for _, line := range strings.Split(strings.TrimSuffix(input, "\n"), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "?") {
			path := strings.TrimSpace(strings.TrimPrefix(line, "?"))
			entries = append(entries, gitStatusEntry{Path: path, Status: "?", Worktree: "?", Unstaged: true})
			continue
		}
		if strings.HasPrefix(line, "!") {
			continue
		}
		parts := strings.SplitN(line, " ", 10)
		if len(parts) < 3 {
			continue
		}
		kind := parts[0]
		if kind != "1" && kind != "2" && kind != "u" {
			continue
		}
		xy := parts[1]
		if len(xy) < 2 {
			continue
		}
		path := parts[len(parts)-1]
		orig := ""
		if kind == "2" {
			if len(parts) < 10 {
				continue
			}
			pathFields := strings.SplitN(parts[9], "\t", 2)
			path = pathFields[0]
			if len(pathFields) == 2 {
				orig = pathFields[1]
			}
		}
		index, worktree := string(xy[0]), string(xy[1])
		status := index
		if status == "." {
			status = worktree
		}
		entries = append(entries, gitStatusEntry{Path: path, OrigPath: orig, Index: index, Worktree: worktree, Status: status, Staged: index != ".", Unstaged: worktree != "."})
	}
	return entries
}

func parseGitLog(input string) []gitLogRow {
	rows := []gitLogRow{}
	for _, line := range strings.Split(input, "\n") {
		at := strings.IndexByte(line, 0)
		if at < 0 {
			continue
		}
		prefix := line[:at]
		fields := strings.SplitN(line[at+1:], "\x00", 5)
		if len(fields) != 5 {
			continue
		}
		words := strings.Fields(prefix)
		if len(words) == 0 {
			continue
		}
		sha := words[len(words)-1]
		graph := strings.TrimSuffix(prefix, sha)
		graph = strings.TrimSuffix(graph, " ")
		parents := strings.Fields(fields[0])
		refs := []string{}
		if strings.TrimSpace(fields[1]) != "" {
			for _, ref := range strings.Split(fields[1], ", ") {
				if ref = strings.TrimSpace(ref); ref != "" {
					refs = append(refs, ref)
				}
			}
		}
		rows = append(rows, gitLogRow{SHA: sha, Parents: parents, Refs: refs, Author: fields[2], When: fields[3], Subject: fields[4], Graph: graph})
	}
	return rows
}

func (a *app) gitWorktrees(repo string) []gitWorktree {
	out, err := gitCommand(context.Background(), repo, "worktree", "list", "--porcelain")
	if err != nil {
		return []gitWorktree{}
	}
	result := []gitWorktree{}
	var current *gitWorktree
	flush := func() {
		if current != nil {
			result = append(result, *current)
			current = nil
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		switch {
		case strings.HasPrefix(line, "worktree "):
			flush()
			current = &gitWorktree{Path: strings.TrimPrefix(line, "worktree ")}
		case strings.HasPrefix(line, "branch ") && current != nil:
			current.Branch = strings.TrimPrefix(line, "branch refs/heads/")
		case line == "detached" && current != nil:
			current.Branch = "(detached)"
		case line == "":
			flush()
		}
	}
	flush()
	for i := range result {
		if pathWithin(a.cfg.FilesRoot, result[i].Path) {
			rel, relErr := filepath.Rel(a.cfg.FilesRoot, result[i].Path)
			if relErr == nil {
				result[i].FilesPath = "/" + filepath.ToSlash(rel)
			}
		}
		status, statusErr := gitStatus(result[i].Path)
		if statusErr == nil {
			result[i].Dirty = len(status)
		}
	}
	return result
}

func (a *app) gitAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/git/repos":
		if requireMethod(w, r, http.MethodGet) {
			jsonReply(w, http.StatusOK, a.discoverGitRepos())
		}
	case "/api/git/log":
		a.gitLogAPI(w, r)
	case "/api/git/status":
		a.gitStatusAPI(w, r)
	case "/api/git/diff":
		a.gitDiffAPI(w, r)
	case "/api/git/stage":
		a.gitStageAPI(w, r, true)
	case "/api/git/unstage":
		a.gitStageAPI(w, r, false)
	case "/api/git/commit":
		a.gitCommitAPI(w, r)
	case "/api/git/checkout":
		a.gitCheckoutAPI(w, r)
	default:
		jsonReply(w, http.StatusNotFound, object{"error": "Git route not found"})
	}
}

func (a *app) gitRepoQuery(w http.ResponseWriter, r *http.Request) (string, bool) {
	repo, err := a.validateGitRepo(r.URL.Query().Get("repo"))
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
		return "", false
	}
	return repo, true
}

func (a *app) gitLogAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	repo, ok := a.gitRepoQuery(w, r)
	if !ok {
		return
	}
	n := 60
	if raw := r.URL.Query().Get("n"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 200 {
			jsonReply(w, http.StatusBadRequest, object{"error": "n must be between 1 and 200"})
			return
		}
		n = parsed
	}
	out, err := gitCommand(r.Context(), repo, "log", "--graph", "--date=iso-strict", "--format=%H%x00%P%x00%D%x00%an%x00%aI%x00%s", "-n", strconv.Itoa(n))
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	jsonReply(w, http.StatusOK, parseGitLog(string(out)))
}

func (a *app) gitStatusAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	repo, ok := a.gitRepoQuery(w, r)
	if !ok {
		return
	}
	status, err := a.cachedGitStatus(repo)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	jsonReply(w, http.StatusOK, status)
}

func (a *app) gitPath(repo, raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if filepath.IsAbs(raw) || strings.ContainsRune(raw, 0) {
		return "", fmt.Errorf("path must be inside the repo")
	}
	clean := filepath.Clean(filepath.FromSlash(raw))
	if clean == "." || !pathWithin(repo, filepath.Join(repo, clean)) {
		return "", fmt.Errorf("path must be inside the repo")
	}
	return filepath.ToSlash(clean), nil
}

func (a *app) gitDiffAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}
	repo, ok := a.gitRepoQuery(w, r)
	if !ok {
		return
	}
	path, err := a.gitPath(repo, r.URL.Query().Get("path"))
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
		return
	}
	args := []string{"diff", "--no-ext-diff"}
	if r.URL.Query().Get("staged") == "true" {
		args = append(args, "--cached")
	}
	args = append(args, "--")
	if path != "" {
		args = append(args, path)
	}
	out, err := gitCommand(r.Context(), repo, args...)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	truncated := len(out) > gitDiffLimit
	if truncated {
		out = out[:gitDiffLimit]
	}
	jsonReply(w, http.StatusOK, object{"diff": string(out), "truncated": truncated})
}

type gitPathsRequest struct {
	Repo  string   `json:"repo"`
	Path  string   `json:"path"`
	Paths []string `json:"paths"`
}

func (a *app) gitStageAPI(w http.ResponseWriter, r *http.Request, stage bool) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body gitPathsRequest
	if !decodeJSONBody(w, r, &body, 64<<10) {
		return
	}
	if body.Path != "" {
		body.Paths = append(body.Paths, body.Path)
	}
	if len(body.Paths) == 0 {
		jsonReply(w, http.StatusBadRequest, object{"error": "path is required"})
		return
	}
	repo, err := a.validateGitRepo(body.Repo)
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
		return
	}
	paths := make([]string, 0, len(body.Paths))
	for _, raw := range body.Paths {
		path, pathErr := a.gitPath(repo, raw)
		if pathErr != nil || path == "" {
			jsonReply(w, http.StatusBadRequest, object{"error": "path must be inside the repo"})
			return
		}
		paths = append(paths, path)
	}
	args := []string{}
	if stage {
		args = append(args, "add")
	} else {
		args = append(args, "restore", "--staged")
	}
	args = append(args, "--")
	args = append(args, paths...)
	if _, err := gitCommand(r.Context(), repo, args...); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	a.invalidateGitStatus(repo)
	jsonReply(w, http.StatusOK, object{"ok": true})
}

func (a *app) gitCommitAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Repo    string `json:"repo"`
		Message string `json:"message"`
	}
	if !decodeJSONBody(w, r, &body, 16<<10) {
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "commit message is required"})
		return
	}
	repo, err := a.validateGitRepo(body.Repo)
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
		return
	}
	for _, key := range []string{"user.name", "user.email"} {
		if value, configErr := gitCommand(r.Context(), repo, "config", "--get", key); configErr != nil || strings.TrimSpace(string(value)) == "" {
			jsonReply(w, http.StatusBadRequest, object{"error": "run git config user.name in this repo"})
			return
		}
	}
	if _, err := gitCommand(r.Context(), repo, "commit", "-m", body.Message); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	a.invalidateGitStatus(repo)
	jsonReply(w, http.StatusOK, object{"ok": true})
}

func (a *app) gitCheckoutAPI(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}
	var body struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if !decodeJSONBody(w, r, &body, 16<<10) {
		return
	}
	if strings.TrimSpace(body.Branch) == "" || strings.HasPrefix(body.Branch, "-") || strings.ContainsAny(body.Branch, "\x00\r\n\t ") {
		jsonReply(w, http.StatusBadRequest, object{"error": "branch is required"})
		return
	}
	repo, err := a.validateGitRepo(body.Repo)
	if err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": err.Error()})
		return
	}
	status, err := gitStatus(repo)
	if err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	if len(status) != 0 {
		jsonReply(w, http.StatusConflict, object{"error": "refuse checkout while the repo is dirty"})
		return
	}
	if _, err := gitCommand(r.Context(), repo, "check-ref-format", "--branch", body.Branch); err != nil {
		jsonReply(w, http.StatusBadRequest, object{"error": "invalid branch"})
		return
	}
	if _, err := gitCommand(r.Context(), repo, "checkout", body.Branch); err != nil {
		jsonReply(w, http.StatusBadGateway, object{"error": err.Error()})
		return
	}
	a.invalidateGitStatus(repo)
	jsonReply(w, http.StatusOK, object{"ok": true, "branch": body.Branch})
}
