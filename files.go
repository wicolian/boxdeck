package main

import (
	"fmt"
	"html"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

const cspDoc = "sandbox; default-src 'none'; style-src 'unsafe-inline'; img-src * data:"
const cspRaw = "sandbox allow-scripts allow-popups allow-modals"
const fileCSS = `<style>body{max-width:860px;margin:2rem auto;padding:0 1rem;font:16px/1.6 -apple-system,system-ui,sans-serif;color:#222;background:#fff}pre{background:#f4f4f4;padding:.8rem;overflow:auto;border-radius:6px}code{font-size:.9em}table{border-collapse:collapse}td,th{border:1px solid #ddd;padding:.3rem .6rem}img{max-width:100%}a{color:#0a58ca}ul.ls{list-style:none;padding:0}ul.ls a{display:block;min-height:44px;padding:10px 0}@media(max-width:820px){ul.ls a{min-height:56px}}@media(prefers-color-scheme:dark){body{background:#111;color:#ddd}pre{background:#1e1e1e}a{color:#7ab7ff}td,th{border-color:#333}}</style>`

func fileDocument(title, body string) string {
	return `<!doctype html><html lang="en"><meta name="viewport" content="width=device-width,initial-scale=1"><meta charset="utf-8"><title>` + html.EscapeString(title) + `</title>` + fileCSS + body + `</html>`
}

// Resolve against the lexical root before following symlinks, matching the Node
// viewer's intentional support for ~/box -> a mount. Never clean a URL in a mux.
func filePath(root, relative string) (string, error) {
	abs := filepath.Join(root, filepath.FromSlash(relative))
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || strings.ContainsRune(relative, 0) {
		return "", fmt.Errorf("path is outside the files root")
	}
	return abs, nil
}
func (a *app) files(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", cspDoc)
	if r.Method != "GET" && r.Method != "HEAD" {
		http.Error(w, "Use GET to view files", 405)
		return
	}
	relative := strings.TrimPrefix(r.URL.Path, "/files/")
	abs, err := filePath(a.cfg.FilesRoot, relative)
	if err != nil {
		http.Error(w, "forbidden", 403)
		return
	}
	f, err := os.Open(abs)
	if err != nil {
		http.Error(w, "not found: "+r.URL.Path, 404)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	if st.IsDir() {
		if !strings.HasSuffix(r.URL.Path, "/") {
			u := *r.URL
			u.Path += "/"
			u.RawPath = ""
			http.Redirect(w, r, u.String(), 302)
			return
		}
		entries, err := os.ReadDir(abs)
		if err != nil {
			http.Error(w, "Cannot read this folder", 403)
			return
		}
		sort.Slice(entries, func(i, j int) bool {
			if entries[i].IsDir() != entries[j].IsDir() {
				return entries[i].IsDir()
			}
			return strings.ToLower(entries[i].Name()) < strings.ToLower(entries[j].Name())
		})
		body := "<h2>" + html.EscapeString("/"+relative) + "</h2><ul class=\"ls\">"
		if relative != "" {
			body += `<li><a href="../">../</a></li>`
		}
		count := 0
		for _, e := range entries {
			if strings.HasPrefix(e.Name(), ".") || e.Name() == "node_modules" {
				continue
			}
			suffix := ""
			if e.IsDir() {
				suffix = "/"
			}
			body += `<li><a href="` + html.EscapeString(url.PathEscape(e.Name())) + suffix + `">` + html.EscapeString(e.Name()) + suffix + `</a></li>`
			count++
		}
		body += "</ul>"
		if count == 0 {
			body += "<p>This folder is empty. Save a report here to see it.</p>"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			io.WriteString(w, fileDocument("/"+relative, body))
		}
		return
	}
	if !st.Mode().IsRegular() {
		http.Error(w, "Only regular files can be viewed", 403)
		return
	}
	ext := strings.ToLower(filepath.Ext(abs))
	if ext == ".md" || ext == ".markdown" {
		if st.Size() > 8<<20 {
			http.Error(w, "This report is too large to render; open it on the box", 413)
			return
		}
		b, err := io.ReadAll(io.LimitReader(f, 8<<20))
		if err != nil {
			http.Error(w, "Cannot read report", 500)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if r.Method != "HEAD" {
			io.WriteString(w, fileDocument(filepath.Base(abs), `<p><a href="./">`+html.EscapeString(path.Dir(r.URL.Path))+`/</a></p>`+markdown(string(b))))
		}
		return
	}
	typ := mime.TypeByExtension(ext)
	switch ext {
	case ".txt", ".log", ".csv":
		typ = "text/plain; charset=utf-8"
	case ".js":
		typ = "text/javascript"
	}
	if typ == "" {
		typ = "application/octet-stream"
	}
	w.Header().Set("Content-Type", typ)
	if ext == ".html" || ext == ".svg" {
		w.Header().Set("Content-Security-Policy", cspRaw)
	}
	http.ServeContent(w, r, st.Name(), st.ModTime(), f)
}
func (a *app) oldFiles(w http.ResponseWriter, r *http.Request) {
	// Old report origins only redirect. They never disclose files or request a
	// second login, even if filesAuth:false was present in the old config.
	if _, err := filePath(a.cfg.FilesRoot, r.URL.Path); err != nil {
		http.Error(w, "forbidden", 403)
		return
	}
	u := url.URL{Scheme: "http", Host: net.JoinHostPort(a.cfg.Host, strconv.Itoa(int(a.cfg.Port))), Path: "/files/" + strings.TrimPrefix(r.URL.Path, "/"), RawQuery: r.URL.RawQuery}
	if strings.HasPrefix(r.URL.Path, "/files/") {
		u.Path = r.URL.Path
	}
	http.Redirect(w, r, u.String(), 302)
}
