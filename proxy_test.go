package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTerminalHTTPStripsCredentials(t *testing.T) {
	a := testApp(t)
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/term/token" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			t.Errorf("leaked auth or wrong path")
		}
		fmt.Fprint(w, "token")
	}))
	defer up.Close()
	a.cfg.TTYDPort = portNumber(up.Listener.Addr().(*net.TCPAddr).Port)
	r := httptest.NewRequest("GET", "/term/token", nil)
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("Cookie", "boxdeck=secret")
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 200 || w.Body.String() != "token" {
		t.Fatalf("proxy: %d %s", w.Code, w.Body.String())
	}
}
func TestTerminalWebSocketBufferedDuplex(t *testing.T) {
	a := testApp(t)
	up, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer up.Close()
	a.cfg.TTYDPort = portNumber(up.Addr().(*net.TCPAddr).Port)
	upstreamDone := make(chan error, 1)
	go func() {
		conn, err := up.Accept()
		if err != nil {
			upstreamDone <- err
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(3 * time.Second))
		reader := bufio.NewReader(conn)
		r, err := http.ReadRequest(reader)
		if err != nil {
			upstreamDone <- err
			return
		}
		if r.URL.Path != "/term/ws" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
			upstreamDone <- fmt.Errorf("wrong path or leaked auth")
			return
		}
		io.WriteString(conn, "HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\nready")
		b := make([]byte, 5)
		_, err = io.ReadFull(reader, b)
		if err == nil && string(b) != "hello" {
			err = fmt.Errorf("lost client bytes")
		}
		if err == nil {
			_, err = conn.Write(b)
		}
		upstreamDone <- err
	}()
	deck := httptest.NewServer(a)
	defer deck.Close()
	conn, err := net.Dial("tcp", deck.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	r := httptest.NewRequest("GET", "/term/ws", nil)
	r.Host = deck.Listener.Addr().String()
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	r.Header.Set("Upgrade", "websocket")
	r.Header.Set("Connection", "Upgrade")
	r.Write(conn)
	io.WriteString(conn, "hello")
	reader := bufio.NewReader(conn)
	res, err := http.ReadResponse(reader, r)
	if err != nil {
		t.Fatal(err)
	}
	if res.StatusCode != 101 {
		t.Fatal(res.StatusCode)
	}
	b := make([]byte, 10)
	if _, err = io.ReadFull(reader, b); err != nil || string(b) != "readyhello" {
		t.Fatalf("duplex bytes %q %v", b, err)
	}
	if err = <-upstreamDone; err != nil {
		t.Fatal(err)
	}
}
func TestHerdrSocketProtocol(t *testing.T) {
	// Unix socket paths have a short OS limit; use a short directory under TMPDIR.
	dir, err := os.MkdirTemp("", "h-")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "h.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		var req object
		json.NewDecoder(conn).Decode(&req)
		if req["method"] != "session.snapshot" {
			t.Error("wrong method")
		}
		b, _ := os.ReadFile("testdata/herdr.json")
		json.NewEncoder(conn).Encode(json.RawMessage(b))
	}()
	b, err := herdrRequest(context.Background(), socket, "session.snapshot", object{})
	if err != nil {
		t.Fatal(err)
	}
	if h, err := parseHerdr(b); err != nil || !h.Running {
		t.Fatal(err)
	}
}
func TestFocusRequiresAuthAndHerdr(t *testing.T) {
	a := testApp(t)
	r := httptest.NewRequest("POST", "/api/herdr/focus", strings.NewReader(`{"pane_id":"w1:p1"}`))
	w := httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal(w.Code)
	}
	r = httptest.NewRequest("POST", "/api/herdr/focus", strings.NewReader(`{"pane_id":"w1:p1"}`))
	r.SetBasicAuth(a.cfg.User, a.cfg.Password)
	w = httptest.NewRecorder()
	a.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
}
