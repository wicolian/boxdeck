package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// /api/github is a small read-only GitHub summary for status screens: the open pull requests
// the host's gh account authored, their CI rollup, and failed workflow runs from the last day
// in those repositories. boxdeck uses the gh auth that the host already has, so a screen never
// holds a GitHub token. The result is cached in memory and concurrent requests share one fetch.

const (
	githubCacheTTL      = 5 * time.Minute
	githubErrorCacheTTL = 30 * time.Second
	githubFetchTimeout  = 20 * time.Second
	githubPRLimit       = 50
	githubRunRepoLimit  = 8
	githubNotAuthed     = "gh not authenticated on this host"
)

// ghRunner runs gh with args and returns stdout. Tests inject a fake one.
type ghRunner func(ctx context.Context, args ...string) ([]byte, error)

type githubPR struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Title  string `json:"title"`
	CI     string `json:"ci"`
	URL    string `json:"url"`
	Draft  bool   `json:"draft"`
}

type githubPRs struct {
	Open    int        `json:"open"`
	Failing int        `json:"failing"`
	Pending int        `json:"pending"`
	Passing int        `json:"passing"`
	Items   []githubPR `json:"items"`
}

type githubRun struct {
	Repo      string `json:"repo"`
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	URL       string `json:"url"`
	CreatedAt string `json:"createdAt"`
}

type githubRuns struct {
	Failed int         `json:"failed"`
	Items  []githubRun `json:"items"`
}

type githubSummary struct {
	OK        bool       `json:"ok"`
	Login     string     `json:"login"`
	PRs       githubPRs  `json:"prs"`
	Runs      githubRuns `json:"runs"`
	FetchedAt string     `json:"fetchedAt"`
	Error     string     `json:"error,omitempty"`
}

type githubService struct {
	run ghRunner
	now func() time.Time

	mu       sync.Mutex
	cached   *githubSummary
	cachedAt time.Time
	inflight chan struct{}
}

func newGitHubService(run ghRunner) *githubService {
	return &githubService{run: run, now: time.Now}
}

// get returns the cached summary, or waits for one shared fetch. It never blocks other
// endpoints: the fetch runs on its own goroutine with its own timeout.
func (s *githubService) get(ctx context.Context) githubSummary {
	s.mu.Lock()
	if s.cached != nil {
		ttl := githubCacheTTL
		if !s.cached.OK {
			ttl = githubErrorCacheTTL
		}
		if s.now().Sub(s.cachedAt) < ttl {
			out := *s.cached
			s.mu.Unlock()
			return out
		}
	}
	wait := s.inflight
	if wait == nil {
		wait = make(chan struct{})
		s.inflight = wait
		go s.refresh(wait)
	}
	s.mu.Unlock()
	select {
	case <-wait:
	case <-ctx.Done():
		return githubSummary{Error: "request cancelled", FetchedAt: s.now().UTC().Format(time.RFC3339)}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.cached
}

func (s *githubService) refresh(done chan struct{}) {
	ctx, cancel := context.WithTimeout(context.Background(), githubFetchTimeout)
	defer cancel()
	summary := fetchGitHubSummary(ctx, s.run, s.now())
	s.mu.Lock()
	s.cached, s.cachedAt, s.inflight = &summary, s.now(), nil
	s.mu.Unlock()
	close(done)
}

const githubPRQuery = `query($q:String!,$n:Int!){viewer{login} search(query:$q,type:ISSUE,first:$n){nodes{... on PullRequest{number title url isDraft repository{nameWithOwner} commits(last:1){nodes{commit{statusCheckRollup{state}}}}}}}}`

type githubGraphQL struct {
	Data struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
		Search struct {
			Nodes []struct {
				Number     int    `json:"number"`
				Title      string `json:"title"`
				URL        string `json:"url"`
				IsDraft    bool   `json:"isDraft"`
				Repository struct {
					NameWithOwner string `json:"nameWithOwner"`
				} `json:"repository"`
				Commits struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								State string `json:"state"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"nodes"`
		} `json:"search"`
	} `json:"data"`
	Errors []struct {
		Message string `json:"message"`
	} `json:"errors"`
}

// ciState maps a GitHub statusCheckRollup state to the four values the screen shows.
func ciState(rollup string) string {
	switch strings.ToUpper(rollup) {
	case "SUCCESS":
		return "passing"
	case "FAILURE", "ERROR":
		return "failing"
	case "PENDING", "EXPECTED":
		return "pending"
	default:
		return "none"
	}
}

// parseGitHubPRs turns the GraphQL reply into the login and the counted PR list.
func parseGitHubPRs(raw []byte) (string, githubPRs, error) {
	var reply githubGraphQL
	if err := json.Unmarshal(raw, &reply); err != nil {
		return "", githubPRs{}, errors.New("gh returned data that is not JSON")
	}
	if len(reply.Errors) > 0 && reply.Data.Viewer.Login == "" {
		return "", githubPRs{}, errors.New("GitHub: " + firstLine(reply.Errors[0].Message))
	}
	prs := githubPRs{Items: []githubPR{}}
	for _, node := range reply.Data.Search.Nodes {
		if node.Number == 0 {
			continue // a search hit that is not a pull request
		}
		state := ""
		if n := len(node.Commits.Nodes); n > 0 && node.Commits.Nodes[n-1].Commit.StatusCheckRollup != nil {
			state = node.Commits.Nodes[n-1].Commit.StatusCheckRollup.State
		}
		pr := githubPR{Repo: node.Repository.NameWithOwner, Number: node.Number, Title: node.Title, CI: ciState(state), URL: node.URL, Draft: node.IsDraft}
		switch pr.CI {
		case "failing":
			prs.Failing++
		case "pending":
			prs.Pending++
		case "passing":
			prs.Passing++
		}
		prs.Items = append(prs.Items, pr)
	}
	prs.Open = len(prs.Items)
	return reply.Data.Viewer.Login, prs, nil
}

type githubRunsReply struct {
	WorkflowRuns []struct {
		Name       string `json:"name"`
		HeadBranch string `json:"head_branch"`
		HTMLURL    string `json:"html_url"`
		CreatedAt  string `json:"created_at"`
	} `json:"workflow_runs"`
}

func parseGitHubRuns(repo string, raw []byte) ([]githubRun, error) {
	var reply githubRunsReply
	if err := json.Unmarshal(raw, &reply); err != nil {
		return nil, err
	}
	runs := make([]githubRun, 0, len(reply.WorkflowRuns))
	for _, r := range reply.WorkflowRuns {
		runs = append(runs, githubRun{Repo: repo, Name: r.Name, Branch: r.HeadBranch, URL: r.HTMLURL, CreatedAt: r.CreatedAt})
	}
	return runs, nil
}

func fetchGitHubSummary(ctx context.Context, run ghRunner, now time.Time) githubSummary {
	out := githubSummary{PRs: githubPRs{Items: []githubPR{}}, Runs: githubRuns{Items: []githubRun{}}, FetchedAt: now.UTC().Format(time.RFC3339)}
	raw, err := run(ctx, "api", "graphql",
		"-f", "query="+githubPRQuery,
		"-f", "q=is:pr is:open author:@me archived:false sort:updated-desc",
		"-F", "n="+strconv.Itoa(githubPRLimit))
	if err != nil {
		out.Error = err.Error()
		return out
	}
	login, prs, err := parseGitHubPRs(raw)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.OK, out.Login, out.PRs = true, login, prs

	// Failed workflow runs by this account in the last day, in the repositories with open PRs.
	// This part is best-effort: a failure here leaves runs empty and ok true.
	repos := []string{}
	seen := map[string]bool{}
	for _, pr := range prs.Items {
		if pr.Repo != "" && !seen[pr.Repo] && len(repos) < githubRunRepoLimit {
			seen[pr.Repo] = true
			repos = append(repos, pr.Repo)
		}
	}
	since := now.Add(-24 * time.Hour).UTC().Format("2006-01-02T15:04:05Z")
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, repo := range repos {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			q := url.Values{"actor": {login}, "status": {"failure"}, "created": {">=" + since}, "per_page": {"10"}}
			raw, err := run(ctx, "api", "repos/"+repo+"/actions/runs?"+q.Encode())
			if err != nil {
				return
			}
			runs, err := parseGitHubRuns(repo, raw)
			if err != nil {
				return
			}
			mu.Lock()
			out.Runs.Items = append(out.Runs.Items, runs...)
			mu.Unlock()
		}(repo)
	}
	wg.Wait()
	sort.Slice(out.Runs.Items, func(i, j int) bool { return out.Runs.Items[i].CreatedAt > out.Runs.Items[j].CreatedAt })
	out.Runs.Failed = len(out.Runs.Items)
	return out
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

// ghBinary finds gh. A launchd or systemd service often has a short PATH, so the usual
// install places are tried too.
func ghBinary() string {
	if path, err := exec.LookPath("gh"); err == nil {
		return path
	}
	for _, path := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh", "/usr/bin/gh", "/home/linuxbrew/.linuxbrew/bin/gh"} {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// runGH is the real ghRunner. Errors carry no gh output beyond one short line, so no
// credential can reach the response.
func runGH(ctx context.Context, args ...string) ([]byte, error) {
	binary := ghBinary()
	if binary == "" {
		return nil, errors.New(githubNotAuthed)
	}
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GH_NO_UPDATE_NOTIFIER=1", "NO_COLOR=1")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err == nil {
		return out, nil
	}
	if ctx.Err() != nil {
		return nil, errors.New("gh timed out")
	}
	msg := strings.ToLower(stderr.String())
	if strings.Contains(msg, "auth login") || strings.Contains(msg, "not logged") || strings.Contains(msg, "http 401") || strings.Contains(msg, "bad credentials") {
		return nil, errors.New(githubNotAuthed)
	}
	if line := firstLine(stderr.String()); line != "" {
		return nil, errors.New("gh failed: " + line)
	}
	return nil, errors.New("gh failed")
}

func (a *app) githubAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET for this endpoint"})
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	jsonReply(w, http.StatusOK, a.github.get(r.Context()))
}
