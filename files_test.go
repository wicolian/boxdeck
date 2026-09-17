package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFilesBoundaryRawAndEncoded(t *testing.T) {
	a := testApp(t)
	os.WriteFile(filepath.Join(a.cfg.FilesRoot, "report.md"), []byte("# Report"), 0600)
	os.Mkdir(filepath.Join(a.cfg.FilesRoot, "folder"), 0700)
	for _, tt := range []struct {
		path string
		want int
	}{{"/../etc/passwd", 403}, {"/files/../etc/passwd", 403}, {"/files/%2e%2e%2Fetc/passwd", 403}, {"/files/%2F..%2Fetc/passwd", 403}, {"/files/%2e%2e%2f..%2fetc/passwd", 403}, {"/files/%00", 403}, {"/files/report.md", 200}, {"/files/%2Freport.md", 200}, {"/files/folder", 302}, {"/files/folder/", 200}, {"/files/missing.md", 404}} {
		w := request(a, "GET", tt.path, true)
		if w.Code != tt.want {
			t.Errorf("raw %s: got %d want %d", tt.path, w.Code, tt.want)
		}
	}
	w := request(a, "GET", "/files/report.md", true)
	if w.Header().Get("Content-Security-Policy") != cspDoc || w.Header().Get("X-Content-Type-Options") != "nosniff" || !strings.Contains(w.Body.String(), "<h1>Report</h1>") {
		t.Fatal("Markdown response")
	}
	w = request(a, "GET", "/files/folder?x=1", true)
	if w.Header().Get("Location") != "/files/folder/?x=1" {
		t.Fatal("folder redirect")
	}
}
func TestFilesSymlinkAndSandbox(t *testing.T) {
	a := testApp(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "demo.html"), []byte("<script>parent.document</script>"), 0600)
	if err := os.Symlink(outside, filepath.Join(a.cfg.FilesRoot, "mount")); err != nil {
		t.Fatal(err)
	}
	w := request(a, "GET", "/files/mount/demo.html", true)
	if w.Code != 200 || w.Header().Get("Content-Security-Policy") != cspRaw || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("intentional mount symlink or HTML sandbox lost")
	}
	r := httptest.NewRequest("GET", "http://old:8090/codes/a%20b.md", nil)
	w = httptest.NewRecorder()
	a.oldFiles(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "http://box:8103/files/codes/a%20b.md" {
		t.Fatal("legacy redirect")
	}
}
func TestMarkdown(t *testing.T) {
	input := "# Heading\n\nA **bold** and *italic* paragraph with `code` and [link](https://example.com).\n\n- one\n- two\n\n1. first\n2. second\n\n```go\n<script>alert(1)</script>\n```\n\n| A | B |\n| --- | :---: |\n| 1 | 2 |\n\n![image](pic.png)\n"
	out := markdown(input)
	for _, want := range []string{"<h1>Heading</h1>", "<strong>bold</strong>", "<em>italic</em>", "<code>code</code>", `href="https://example.com"`, "<ul><li>one</li><li>two</li></ul>", "<ol><li>first</li>", "&lt;script&gt;", "<th>A</th>", "<td>2</td>", `<img alt="image" src="pic.png">`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in %s", want, out)
		}
	}
	bad := markdown(`<script>alert(1)</script> [x](javascript:alert) [x](javascript&#58;alert) ![x](data:text/html,bad)`)
	if strings.Contains(bad, "<script>") || strings.Contains(bad, `href="javascript`) || strings.Contains(bad, `src="data:`) {
		t.Fatal("executable Markdown")
	}
}
