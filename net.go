package main

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type netNode struct {
	Name    string   `json:"name"`
	DNSName string   `json:"dnsName,omitempty"`
	OS      string   `json:"os,omitempty"`
	IPs     []string `json:"ips"`
	Online  bool     `json:"online"`
}

type tailscaleStatus struct {
	Self  netNode   `json:"self"`
	Peers []netNode `json:"peers"`
}

type tailscaleServeEntry struct {
	Protocol string `json:"protocol"`
	Address  string `json:"address"`
	Path     string `json:"path,omitempty"`
	Target   string `json:"target"`
}

type netInterface struct {
	Name string   `json:"name"`
	IPv4 []string `json:"ipv4"`
}

func nodeIPs(value []string) []string {
	result := append([]string(nil), value...)
	sort.Strings(result)
	return result
}

func parseTailscaleStatus(data []byte) (tailscaleStatus, error) {
	var raw struct {
		Self struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			OS           string   `json:"OS"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			Online       bool     `json:"Online"`
		} `json:"Self"`
		Peer map[string]struct {
			HostName     string   `json:"HostName"`
			DNSName      string   `json:"DNSName"`
			OS           string   `json:"OS"`
			TailscaleIPs []string `json:"TailscaleIPs"`
			Online       bool     `json:"Online"`
		} `json:"Peer"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return tailscaleStatus{}, err
	}
	status := tailscaleStatus{Self: netNode{Name: raw.Self.HostName, DNSName: strings.TrimSuffix(raw.Self.DNSName, "."), OS: raw.Self.OS, IPs: nodeIPs(raw.Self.TailscaleIPs), Online: raw.Self.Online}, Peers: []netNode{}}
	for _, peer := range raw.Peer {
		status.Peers = append(status.Peers, netNode{Name: peer.HostName, DNSName: strings.TrimSuffix(peer.DNSName, "."), OS: peer.OS, IPs: nodeIPs(peer.TailscaleIPs), Online: peer.Online})
	}
	sort.Slice(status.Peers, func(i, j int) bool { return status.Peers[i].Name < status.Peers[j].Name })
	return status, nil
}

func serveTarget(value any) string {
	if object, ok := value.(map[string]any); ok {
		for _, key := range []string{"Proxy", "Handler", "Target", "Serve"} {
			if target, ok := object[key].(string); ok && target != "" {
				return target
			}
		}
		for _, child := range object {
			if target := serveTarget(child); target != "" {
				return target
			}
		}
	}
	if list, ok := value.([]any); ok {
		for _, child := range list {
			if target := serveTarget(child); target != "" {
				return target
			}
		}
	}
	return ""
}

func parseTailscaleServeStatus(data []byte) ([]tailscaleServeEntry, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	entries := []tailscaleServeEntry{}
	if web, ok := raw["Web"].(map[string]any); ok {
		for address, value := range web {
			if object, ok := value.(map[string]any); ok {
				if handlers, ok := object["Handlers"].(map[string]any); ok {
					for path, handler := range handlers {
						if target := serveTarget(handler); target != "" {
							entries = append(entries, tailscaleServeEntry{Protocol: "web", Address: address, Path: path, Target: target})
						}
					}
					continue
				}
			}
			if target := serveTarget(value); target != "" {
				entries = append(entries, tailscaleServeEntry{Protocol: "web", Address: address, Target: target})
			}
		}
	}
	if tcp, ok := raw["TCP"].(map[string]any); ok {
		for address, value := range tcp {
			if target := serveTarget(value); target != "" {
				entries = append(entries, tailscaleServeEntry{Protocol: "tcp", Address: address, Target: target})
			}
		}
	}
	if len(entries) == 0 {
		if target := serveTarget(raw); target != "" {
			entries = append(entries, tailscaleServeEntry{Protocol: "serve", Target: target})
		}
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Protocol != entries[j].Protocol {
			return entries[i].Protocol < entries[j].Protocol
		}
		return entries[i].Address+entries[i].Path < entries[j].Address+entries[j].Path
	})
	return entries, nil
}

func localInterfaces() []netInterface {
	interfaces, err := net.Interfaces()
	if err != nil {
		return []netInterface{}
	}
	result := []netInterface{}
	for _, iface := range interfaces {
		addresses, err := iface.Addrs()
		if err != nil {
			continue
		}
		ips := []string{}
		for _, address := range addresses {
			var ip net.IP
			switch value := address.(type) {
			case *net.IPNet:
				ip = value.IP
			case *net.IPAddr:
				ip = value.IP
			}
			if v4 := ip.To4(); v4 != nil && !v4.IsLoopback() {
				ips = append(ips, v4.String())
			}
		}
		if len(ips) > 0 {
			sort.Strings(ips)
			result = append(result, netInterface{Name: iface.Name, IPv4: ips})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Name < result[j].Name })
	return result
}

type netViewState struct {
	Tailscale    object                `json:"tailscale"`
	Serve        []tailscaleServeEntry `json:"serve"`
	Interfaces   []netInterface        `json:"interfaces"`
	Mirrors      []int                 `json:"mirrors"`
	MirrorErrors []string              `json:"mirrorErrors"`
	MirrorBind   string                `json:"mirrorBind,omitempty"`
	MirrorRule   string                `json:"mirrorRule"`
	Devices      []peerDevice          `json:"devices"`
	GeneratedAt  string                `json:"generatedAt"`
}

func (a *app) netState(ctx context.Context) netViewState {
	result := netViewState{Interfaces: localInterfaces(), Mirrors: []int{}, MirrorErrors: []string{}, Devices: []peerDevice{}, MirrorRule: "Mirrored ports carry no password", GeneratedAt: time.Now().UTC().Format(time.RFC3339)}
	status := tailscaleStatus{Peers: []netNode{}}
	if path, err := exec.LookPath("tailscale"); err == nil {
		result.Tailscale = object{"present": true, "message": ""}
		if output, err := commandWithTimeout(ctx, 4*time.Second, path, "status", "--json"); err == nil {
			if parsed, parseErr := parseTailscaleStatus(output); parseErr == nil {
				status = parsed
				result.Tailscale["running"] = true
				result.Tailscale["self"] = parsed.Self
				result.Tailscale["peers"] = parsed.Peers
			} else {
				result.Tailscale["message"] = "Tailscale is installed but its status is unavailable"
			}
		} else {
			result.Tailscale["message"] = "Start Tailscale on this box to see peers"
		}
		if output, err := commandWithTimeout(ctx, 4*time.Second, path, "serve", "status", "--json"); err == nil {
			result.Serve, _ = parseTailscaleServeStatus(output)
		}
	} else {
		result.Tailscale = object{"present": false, "running": false, "message": "Install Tailscale to connect this box and its agents"}
	}
	if a.peers != nil {
		result.Devices = a.peers.snapshot(ctx, status, a.cfg.FleetToken)
	}
	result.Mirrors, result.MirrorErrors = a.mirrors.snapshot()
	result.MirrorBind = mirrorIP(a.cfg)
	return result
}

func (a *app) netAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read network state"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	jsonReply(w, http.StatusOK, a.netState(ctx))
}
