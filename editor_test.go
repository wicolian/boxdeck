package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditRejectsPathsOutsideFilesRoot(t *testing.T) {
	a := testApp(t)
	for _, target := range []string{"/api/edit?path=../escape.txt", "/api/edit/new?path=%2e%2e%2Fescape.txt"} {
		method := http.MethodPut
		if strings.Contains(target, "/new?") {
			method = http.MethodPost
		}
		r := httptest.NewRequest(method, target, strings.NewReader("nope"))
		r.SetBasicAuth(a.cfg.User, a.cfg.Password)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if w.Code != http.StatusForbidden {
			t.Fatalf("%s: got %d want %d", target, w.Code, http.StatusForbidden)
		}
	}
}

func TestEditSaveUsesIfMatchAndAtomicRename(t *testing.T) {
	a := testApp(t)
	path := filepath.Join(a.cfg.FilesRoot, "scratch.txt")
	if err := os.WriteFile(path, []byte("before"), 0600); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPut, "/api/edit?path=scratch.txt", strings.NewReader("after"))
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("If-Match", editMtime(st))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("save: got %d %s", w.Code, w.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "after" {
		t.Fatalf("saved bytes: %q %v", got, err)
	}
	if matches, _ := filepath.Glob(path + ".boxdeck-*"); len(matches) != 0 {
		t.Fatalf("temporary save files remain: %v", matches)
	}
	r = httptest.NewRequest(http.MethodPut, "/api/edit?path=scratch.txt", strings.NewReader("stale"))
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("If-Match", "1")
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusPreconditionFailed {
		t.Fatalf("stale save: got %d want %d", w.Code, http.StatusPreconditionFailed)
	}
}

func TestEditDeleteMovesFileToTrash(t *testing.T) {
	a := testApp(t)
	path := filepath.Join(a.cfg.FilesRoot, "remove-me.txt")
	if err := os.WriteFile(path, []byte("keep me"), 0600); err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodDelete, "/api/edit?path=remove-me.txt", nil)
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("delete: got %d %s", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("original still exists: %v", err)
	}
	trash := filepath.Join(a.cfg.home, ".local", "share", "boxdeck", "trash")
	var found bool
	_ = filepath.WalkDir(trash, func(p string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(p) == "remove-me.txt" {
			found = true
		}
		return nil
	})
	if !found {
		t.Fatalf("deleted file was not found under %s", trash)
	}
}
