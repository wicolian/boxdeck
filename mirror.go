package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

func isTailnetIP(addr string) bool {
	ip := net.ParseIP(addr).To4()
	return ip != nil && ip[0] == 100 && ip[1] >= 64 && ip[1] <= 127
}
func mirrorIP(c config) string {
	if c.MirrorBind != "" {
		return c.MirrorBind
	}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		ip, _, err := net.ParseCIDR(a.String())
		if err == nil && isTailnetIP(ip.String()) {
			return ip.String()
		}
	}
	return ""
}

type mirrorEntry struct {
	listener net.Listener
	err      string
	retry    time.Time
}
type mirrorManager struct {
	mu      sync.Mutex
	entries map[int]mirrorEntry
	ctx     context.Context
	cfg     config
}

func (m *mirrorManager) sync(ports []portInfo) {
	ip := mirrorIP(m.cfg)
	if m.cfg.Mirror == false || ip == "" {
		return
	}
	want := map[int]bool{int(m.cfg.Port): true}
	for _, p := range ports {
		// ttyd has no credentials of its own: only /term/ may reach it.
		if !p.Ephemeral && p.Port != int(m.cfg.TTYDPort) {
			want[p.Port] = true
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for port := range want {
		prev, ok := m.entries[port]
		if ok && (prev.listener != nil || time.Now().Before(prev.retry)) {
			continue
		}
		listener, err := net.Listen("tcp", net.JoinHostPort(ip, strconv.Itoa(port)))
		if err != nil {
			m.entries[port] = mirrorEntry{err: fmt.Sprintf("Port %d: %v", port, err), retry: time.Now().Add(time.Minute)}
			continue
		}
		m.entries[port] = mirrorEntry{listener: listener}
		go m.accept(listener, port)
	}
	for port, entry := range m.entries {
		if !want[port] {
			if entry.listener != nil {
				entry.listener.Close()
			}
			delete(m.entries, port)
		}
	}
}
func (m *mirrorManager) accept(listener net.Listener, port int) {
	for {
		client, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			up, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
			if err != nil {
				client.Close()
				return
			}
			pipe(m.ctx, client, client, up, up)
		}()
	}
}
func pipe(ctx context.Context, left net.Conn, leftReader io.Reader, right net.Conn, rightReader io.Reader) {
	defer left.Close()
	defer right.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(left, rightReader); done <- struct{}{} }()
	go func() { io.Copy(right, leftReader); done <- struct{}{} }()
	select {
	case <-ctx.Done():
	case <-done:
	}
	left.Close()
	right.Close() // unblock the other copy, including during shutdown
}
func (m *mirrorManager) snapshot() ([]int, []string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ports := []int{}
	errors := []string{}
	for p, e := range m.entries {
		if e.listener != nil {
			ports = append(ports, p)
		} else if e.err != "" {
			errors = append(errors, e.err)
		}
	}
	sort.Ints(ports)
	sort.Strings(errors)
	return ports, errors
}
func (m *mirrorManager) close() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, entry := range m.entries {
		if entry.listener != nil {
			entry.listener.Close()
		}
	}
}
