package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const maxEditableSize = 1 << 20

type editDocument struct {
	Path     string `json:"path"`
	Content  string `json:"content,omitempty"`
	Size     int64  `json:"size"`
	Mtime    string `json:"mtime"`
	Mode     string `json:"mode"`
	Binary   bool   `json:"binary"`
	ReadOnly bool   `json:"readOnly"`
}

func editMtime(info os.FileInfo) string {
	return strconv.FormatInt(info.ModTime().UnixNano(), 10)
}

func editRelativePath(raw string) string {
	return strings.TrimPrefix(raw, "/")
}

func (a *app) editFilePath(raw string) (string, string, error) {
	relative := editRelativePath(raw)
	abs, err := filePath(a.cfg.FilesRoot, relative)
	return abs, relative, err
}

func editDoc(relative string, info os.FileInfo, content string, binary bool) editDocument {
	return editDocument{
		Path:     relative,
		Content:  content,
		Size:     info.Size(),
		Mtime:    editMtime(info),
		Mode:     info.Mode().String(),
		Binary:   binary,
		ReadOnly: binary || info.Size() > maxEditableSize,
	}
}

func (a *app) edit(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.editRead(w, r)
	case http.MethodPut:
		a.editSave(w, r)
	case http.MethodDelete:
		a.editDelete(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT, DELETE")
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET, PUT or DELETE for this endpoint"})
	}
}

func (a *app) editRead(w http.ResponseWriter, r *http.Request) {
	abs, relative, err := a.editFilePath(r.URL.Query().Get("path"))
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	if !info.Mode().IsRegular() {
		jsonReply(w, http.StatusForbidden, object{"error": "only regular files can be edited"})
		return
	}
	doc := editDoc(relative, info, "", false)
	f, err := os.Open(abs)
	if err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	defer f.Close()
	if info.Size() > maxEditableSize {
		doc.Binary = hasBinaryPrefix(f)
		doc.ReadOnly = true
		jsonReply(w, http.StatusOK, doc)
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, maxEditableSize+1))
	if err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "cannot read file"})
		return
	}
	if len(b) > maxEditableSize {
		doc.ReadOnly = true
		jsonReply(w, http.StatusOK, doc)
		return
	}
	doc.Binary = strings.IndexByte(string(b), 0) >= 0
	doc.ReadOnly = doc.Binary
	if !doc.Binary {
		doc.Content = string(b)
	}
	jsonReply(w, http.StatusOK, doc)
}

func hasBinaryPrefix(r io.Reader) bool {
	b, err := io.ReadAll(io.LimitReader(r, 8192))
	return err == nil && strings.IndexByte(string(b), 0) >= 0
}

func readEditBody(w http.ResponseWriter, r *http.Request) ([]byte, bool) {
	r.Body = http.MaxBytesReader(w, r.Body, maxEditableSize+1)
	b, err := io.ReadAll(r.Body)
	if err != nil || len(b) > maxEditableSize {
		jsonReply(w, http.StatusRequestEntityTooLarge, object{"error": "file is larger than 1 MB"})
		return nil, false
	}
	return b, true
}

func (a *app) editSave(w http.ResponseWriter, r *http.Request) {
	abs, relative, err := a.editFilePath(r.URL.Query().Get("path"))
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	if !info.Mode().IsRegular() {
		jsonReply(w, http.StatusForbidden, object{"error": "only regular files can be edited"})
		return
	}
	if info.Size() > maxEditableSize {
		jsonReply(w, http.StatusRequestEntityTooLarge, object{"error": "files over 1 MB are read-only"})
		return
	}
	match := strings.Trim(r.Header.Get("If-Match"), "\"")
	if match == "" {
		jsonReply(w, http.StatusPreconditionRequired, object{"error": "If-Match is required when saving"})
		return
	}
	if match != editMtime(info) {
		jsonReply(w, http.StatusPreconditionFailed, object{"error": "file changed on disk", "mtime": editMtime(info)})
		return
	}
	b, ok := readEditBody(w, r)
	if !ok {
		return
	}
	if err := atomicEditWrite(abs, b, info.Mode().Perm()); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not save file"})
		return
	}
	updated, err := os.Stat(abs)
	if err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not stat saved file"})
		return
	}
	jsonReply(w, http.StatusOK, editDoc(relative, updated, string(b), false))
}

func atomicEditWrite(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".boxdeck-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func (a *app) editNew(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to create a file"})
		return
	}
	abs, relative, err := a.editFilePath(r.URL.Query().Get("path"))
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	if strings.TrimSpace(relative) == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "a file path is required"})
		return
	}
	if _, err := os.Stat(abs); err == nil {
		jsonReply(w, http.StatusConflict, object{"error": "file already exists"})
		return
	} else if !os.IsNotExist(err) {
		jsonReply(w, http.StatusForbidden, object{"error": "cannot create this path"})
		return
	}
	b, ok := readEditBody(w, r)
	if !ok {
		return
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0700); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "cannot create parent folder"})
		return
	}
	if err := atomicEditWrite(abs, b, 0600); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not create file"})
		return
	}
	info, err := os.Stat(abs)
	if err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not stat created file"})
		return
	}
	jsonReply(w, http.StatusCreated, editDoc(relative, info, string(b), false))
}

func (a *app) editRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use POST to rename a file"})
		return
	}
	var body struct {
		Path    string `json:"path"`
		NewPath string `json:"newPath"`
		To      string `json:"to"`
	}
	if !decodeJSONBody(w, r, &body, 16<<10) {
		return
	}
	if body.NewPath == "" {
		body.NewPath = body.To
	}
	from, fromRelative, err := a.editFilePath(body.Path)
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	to, toRelative, err := a.editFilePath(body.NewPath)
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	if fromRelative == "" || toRelative == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "both file paths are required"})
		return
	}
	if _, err := os.Stat(from); err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	if _, err := os.Stat(to); err == nil {
		jsonReply(w, http.StatusConflict, object{"error": "destination already exists"})
	} else if !os.IsNotExist(err) {
		jsonReply(w, http.StatusForbidden, object{"error": "cannot use destination"})
	} else if err := os.Rename(from, to); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not rename file"})
	} else {
		jsonReply(w, http.StatusOK, object{"path": toRelative})
	}
}

func (a *app) editDelete(w http.ResponseWriter, r *http.Request) {
	abs, relative, err := a.editFilePath(r.URL.Query().Get("path"))
	if err != nil {
		jsonReply(w, http.StatusForbidden, object{"error": "path is outside the files root"})
		return
	}
	if relative == "" {
		jsonReply(w, http.StatusBadRequest, object{"error": "a file path is required"})
		return
	}
	if _, err := os.Stat(abs); err != nil {
		jsonReply(w, http.StatusNotFound, object{"error": "file not found"})
		return
	}
	trashDir := filepath.Join(a.cfg.home, ".local", "share", "boxdeck", "trash", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(trashDir, 0700); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not create trash"})
		return
	}
	destination := filepath.Join(trashDir, filepath.Base(abs))
	for i := 2; ; i++ {
		if _, err := os.Stat(destination); os.IsNotExist(err) {
			break
		}
		destination = filepath.Join(trashDir, fmt.Sprintf("%s-%d", filepath.Base(abs), i))
	}
	if err := os.Rename(abs, destination); err != nil {
		jsonReply(w, http.StatusInternalServerError, object{"error": "could not move file to trash"})
		return
	}
	jsonReply(w, http.StatusOK, object{"path": relative, "trash": destination})
}
