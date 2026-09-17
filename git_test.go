package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseGitPorcelainV2(t *testing.T) {
	input := "1 .M N... 100644 100644 100644 abcdef0 abcdef0 file.txt\n" +
		"1 A. N... 000000 100644 100644 0000000 abcdef1 added.go\n" +
		"2 R. N... 100644 100644 100644 abcdef2 abcdef3 R100 new.go\toriginal.go\n" +
		"? untracked.md\n"
	got := parseGitStatus(input)
	if len(got) != 4 {
		t.Fatalf("got %d entries: %+v", len(got), got)
	}
	if got[0].Path != "file.txt" || got[0].Index != "." || got[0].Worktree != "M" || !got[0].Unstaged {
		t.Fatalf("modified entry: %+v", got[0])
	}
	if got[1].Path != "added.go" || got[1].Index != "A" || !got[1].Staged {
		t.Fatalf("added entry: %+v", got[1])
	}
	if got[2].Path != "new.go" || got[2].OrigPath != "original.go" || got[2].Index != "R" {
		t.Fatalf("rename entry: %+v", got[2])
	}
	if got[3].Path != "untracked.md" || got[3].Status != "?" {
		t.Fatalf("untracked entry: %+v", got[3])
	}
}

func TestParseGitLogGraph(t *testing.T) {
	input := "* tip\x00parent\x00HEAD -> main, origin/main\x00Ada Lovelace\x002026-09-17T20:00:00Z\x00first commit\n" +
		"|\\\n" +
		"| * feature-tip\x00parent-two\x00feature\x00Grace Hopper\x002026-09-17T19:00:00Z\x00feature work\n"
	got := parseGitLog(input)
	if len(got) != 2 {
		t.Fatalf("got %d rows: %+v", len(got), got)
	}
	if got[0].SHA != "tip" || !reflect.DeepEqual(got[0].Parents, []string{"parent"}) || got[0].Refs[0] != "HEAD -> main" || got[0].Subject != "first commit" {
		t.Fatalf("first row: %+v", got[0])
	}
	if got[1].Graph == "" || got[1].Author != "Grace Hopper" || got[1].Refs[0] != "feature" {
		t.Fatalf("second row: %+v", got[1])
	}
}

func TestDiscoverGitReposUnderConfiguredRoots(t *testing.T) {
	a := testApp(t)
	root := t.TempDir()
	a.cfg.RepoRoots = []string{root}
	repo := filepath.Join(root, "nested", "demo")
	if err := os.MkdirAll(repo, 0700); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "init", "-q")
	runGitTest(t, repo, "config", "user.name", "Test User")
	runGitTest(t, repo, "config", "user.email", "test@example.com")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("demo"), 0600); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, repo, "add", "README.md")
	runGitTest(t, repo, "commit", "-m", "initial")
	got := a.discoverGitRepos()
	if len(got) != 1 || got[0].Path != repo || got[0].Branch == "" || got[0].LastCommit.Subject != "initial" {
		t.Fatalf("discovered repos: %+v", got)
	}
}

func runGitTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}
