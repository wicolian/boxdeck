package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const cannedGitHubGraphQL = `{"data":{"viewer":{"login":"octo"},"search":{"nodes":[
{"number":1,"title":"red","url":"https://github.com/o/a/pull/1","isDraft":false,"repository":{"nameWithOwner":"o/a"},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"FAILURE"}}}]}},
{"number":2,"title":"broken","url":"https://github.com/o/a/pull/2","isDraft":true,"repository":{"nameWithOwner":"o/a"},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"ERROR"}}}]}},
{"number":3,"title":"running","url":"https://github.com/o/b/pull/3","isDraft":false,"repository":{"nameWithOwner":"o/b"},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"PENDING"}}}]}},
{"number":4,"title":"waiting","url":"https://github.com/o/b/pull/4","isDraft":false,"repository":{"nameWithOwner":"o/b"},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"EXPECTED"}}}]}},
{"number":5,"title":"green","url":"https://github.com/o/b/pull/5","isDraft":false,"repository":{"nameWithOwner":"o/b"},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"state":"SUCCESS"}}}]}},
{"number":6,"title":"no checks","url":"https://github.com/o/c/pull/6","isDraft":false,"repository":{"nameWithOwner":"o/c"},"commits":{"nodes":[{"commit":{"statusCheckRollup":null}}]}},
{}
]}}}`

const cannedGitHubRuns = `{"total_count":1,"workflow_runs":[{"name":"CI","head_branch":"feat","html_url":"https://github.com/o/a/actions/runs/9","created_at":"2026-10-20T08:00:00Z"}]}`

func fakeGH(calls *atomic.Int32) ghRunner {
	return func(ctx context.Context, args ...string) ([]byte, error) {
		calls.Add(1)
		if len(args) >= 2 && args[0] == "api" && args[1] == "graphql" {
			return []byte(cannedGitHubGraphQL), nil
		}
		if len(args) == 2 && strings.HasPrefix(args[1], "repos/o/a/actions/runs?") {
			if !strings.Contains(args[1], "actor=octo") || !strings.Contains(args[1], "status=failure") {
				return nil, errors.New("unexpected runs query")
			}
			return []byte(cannedGitHubRuns), nil
		}
		return []byte(`{"workflow_runs":[]}`), nil
	}
}

func TestCIStateMapping(t *testing.T) {
	for in, want := range map[string]string{"SUCCESS": "passing", "FAILURE": "failing", "ERROR": "failing", "PENDING": "pending", "EXPECTED": "pending", "": "none", "weird": "none"} {
		if got := ciState(in); got != want {
			t.Errorf("ciState(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitHubSummaryParsesRollup(t *testing.T) {
	var calls atomic.Int32
	s := fetchGitHubSummary(context.Background(), fakeGH(&calls), time.Date(2026, 10, 20, 9, 0, 0, 0, time.UTC))
	if !s.OK || s.Login != "octo" || s.Error != "" {
		t.Fatalf("summary: ok=%v login=%q error=%q", s.OK, s.Login, s.Error)
	}
	if s.PRs.Open != 6 || s.PRs.Failing != 2 || s.PRs.Pending != 2 || s.PRs.Passing != 1 {
		t.Fatalf("counts: %+v", s.PRs)
	}
	want := []string{"failing", "failing", "pending", "pending", "passing", "none"}
	for i, pr := range s.PRs.Items {
		if pr.CI != want[i] {
			t.Errorf("PR %d ci = %q, want %q", pr.Number, pr.CI, want[i])
		}
	}
	if !s.PRs.Items[1].Draft || s.PRs.Items[0].Repo != "o/a" || s.PRs.Items[0].URL == "" {
		t.Fatalf("item fields: %+v", s.PRs.Items[:2])
	}
	if s.Runs.Failed != 1 || s.Runs.Items[0].Repo != "o/a" || s.Runs.Items[0].Name != "CI" {
		t.Fatalf("runs: %+v", s.Runs)
	}
	if calls.Load() != 4 { // one GraphQL call and one runs call per distinct repo
		t.Fatalf("gh calls = %d, want 4", calls.Load())
	}
}

func TestGitHubSummaryNotAuthenticated(t *testing.T) {
	run := func(ctx context.Context, args ...string) ([]byte, error) { return nil, errors.New(githubNotAuthed) }
	s := fetchGitHubSummary(context.Background(), run, time.Now())
	if s.OK || s.Error != githubNotAuthed || s.PRs.Items == nil {
		t.Fatalf("summary: %+v", s)
	}
	b, _ := json.Marshal(s)
	if !strings.Contains(string(b), `"items":[]`) {
		t.Fatalf("items must be an empty list, not null: %s", b)
	}
}

func TestGitHubCacheSharesOneFetch(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	run := func(ctx context.Context, args ...string) ([]byte, error) {
		if args[1] == "graphql" {
			calls.Add(1)
			<-release
			return []byte(cannedGitHubGraphQL), nil
		}
		return []byte(`{"workflow_runs":[]}`), nil
	}
	svc := newGitHubService(run)
	now := time.Now()
	svc.now = func() time.Time { return now }
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s := svc.get(context.Background()); !s.OK {
				t.Errorf("shared fetch: %+v", s)
			}
		}()
	}
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	svc.get(context.Background())
	if calls.Load() != 1 {
		t.Fatalf("GraphQL calls = %d, want 1 (single flight + cache)", calls.Load())
	}
	now = now.Add(githubCacheTTL + time.Second)
	svc.get(context.Background())
	if calls.Load() != 2 {
		t.Fatalf("GraphQL calls after expiry = %d, want 2", calls.Load())
	}
}

func TestReadOnlyTokenReadsGitHub(t *testing.T) {
	a := testApp(t)
	var calls atomic.Int32
	a.github = newGitHubService(fakeGH(&calls))
	full, _ := randomToken()
	readOnly, _ := randomPrefixedToken(readOnlyTokenPrefix)
	a.cfg.Tokens = []string{full}
	a.cfg.ReadOnlyTokens = []string{readOnly}

	w := bearerRequest(a, http.MethodGet, "/api/github", readOnly, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/github with a read-only token: %d", w.Code)
	}
	var s githubSummary
	if err := json.Unmarshal(w.Body.Bytes(), &s); err != nil || !s.OK || s.PRs.Failing != 2 {
		t.Fatalf("body: %v %s", err, w.Body.String())
	}
	if w := bearerRequest(a, http.MethodHead, "/api/github", readOnly, ""); w.Code != http.StatusOK {
		t.Fatalf("HEAD /api/github with a read-only token: %d", w.Code)
	}
	if w := bearerRequest(a, http.MethodPost, "/api/github", readOnly, `{}`); w.Code != http.StatusForbidden {
		t.Fatalf("POST /api/github with a read-only token: %d, want 403", w.Code)
	}
	if w := bearerRequest(a, http.MethodGet, "/api/github/", readOnly, ""); w.Code != http.StatusForbidden {
		t.Fatalf("GET /api/github/ with a read-only token: %d, want 403", w.Code)
	}
	if w := bearerRequest(a, http.MethodGet, "/api/github", full, ""); w.Code != http.StatusOK {
		t.Fatalf("GET /api/github with a full token: %d", w.Code)
	}
	if w := request(a, http.MethodGet, "/api/github", false); w.Code == http.StatusOK {
		t.Fatalf("GET /api/github without auth: %d", w.Code)
	}
}
