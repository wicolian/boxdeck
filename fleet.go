package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"sort"
	"sync"
	"time"
)

type peerDevice struct {
	Name       string   `json:"name"`
	DNSName    string   `json:"dnsName,omitempty"`
	OS         string   `json:"os,omitempty"`
	Online     bool     `json:"online"`
	IPs        []string `json:"ips"`
	URL        string   `json:"url"`
	Boxdeck    bool     `json:"boxdeck"`
	Version    string   `json:"version,omitempty"`
	Discovered bool     `json:"discovered"`
	Usage      object   `json:"usage,omitempty"`
	UsageOK    bool     `json:"usageOk,omitempty"`
	StateOK    bool     `json:"stateOk,omitempty"`
	Health     object   `json:"health,omitempty"`
	Agents     int      `json:"agents,omitempty"`
	Ports      int      `json:"ports,omitempty"`
	Message    string   `json:"message,omitempty"`
}

type peerDiscovery struct {
	mu      sync.Mutex
	at      time.Time
	devices []peerDevice
}

func clonePeerDevices(input []peerDevice) []peerDevice {
	return append([]peerDevice(nil), input...)
}

func peerAddress(node netNode) string {
	for _, ip := range node.IPs {
		if parsed := net.ParseIP(ip); parsed != nil && parsed.To4() != nil {
			return ip
		}
	}
	for _, ip := range node.IPs {
		if net.ParseIP(ip) != nil {
			return ip
		}
	}
	return ""
}

func (d *peerDiscovery) snapshot(ctx context.Context, status tailscaleStatus, fleetToken string) []peerDevice {
	d.mu.Lock()
	if time.Since(d.at) < 60*time.Second {
		value := clonePeerDevices(d.devices)
		d.mu.Unlock()
		return value
	}
	d.mu.Unlock()

	devices := make([]peerDevice, len(status.Peers))
	var wg sync.WaitGroup
	for i, node := range status.Peers {
		wg.Add(1)
		go func(i int, node netNode) {
			defer wg.Done()
			if !node.Online {
				devices[i] = peerDevice{Name: node.Name, DNSName: node.DNSName, OS: node.OS, Online: false, IPs: append([]string(nil), node.IPs...), Discovered: true, Message: "offline"}
				return
			}
			devices[i] = probePeer(ctx, node, fleetToken)
		}(i, node)
	}
	wg.Wait()
	sort.Slice(devices, func(i, j int) bool { return devices[i].Name < devices[j].Name })
	d.mu.Lock()
	d.devices = devices
	d.at = time.Now()
	value := clonePeerDevices(d.devices)
	d.mu.Unlock()
	return value
}

func probePeer(parent context.Context, node netNode, fleetToken string) peerDevice {
	ip := peerAddress(node)
	device := peerDevice{Name: node.Name, DNSName: node.DNSName, OS: node.OS, Online: node.Online, IPs: append([]string(nil), node.IPs...), Discovered: true}
	if device.Name == "" {
		device.Name = ip
	}
	if ip == "" {
		device.Message = "No probe address reported by Tailscale"
		return device
	}
	device.URL = "http://" + net.JoinHostPort(ip, "8100")
	client := &http.Client{Timeout: time.Second}
	requestCtx, cancel := context.WithTimeout(parent, time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, device.URL+"/api/health", nil)
	if err != nil {
		device.Message = "Health probe could not be created"
		return device
	}
	response, err := client.Do(request)
	if err != nil {
		device.Message = "boxdeck not installed"
		return device
	}
	var health struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
	}
	decodeErr := json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&health)
	response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeErr != nil || !health.OK {
		device.Message = "boxdeck not installed"
		return device
	}
	device.Boxdeck, device.Version = true, health.Version
	if fleetToken == "" {
		return device
	}
	fleetClient := &http.Client{Timeout: 3 * time.Second}
	state, stateErr := fetchPeerJSON(parent, fleetClient, device.URL+"/api/state", fleetToken)
	if stateErr == nil {
		device.StateOK = true
		device.Health = mapObject(state["health"])
		device.Agents = collectionLength(state["agents"])
		device.Ports = collectionLength(state["ports"])
	} else {
		device.Message = "fleet token could not read state"
	}
	if usage, usageErr := fetchPeerJSON(parent, fleetClient, device.URL+"/api/usage", fleetToken); usageErr == nil {
		device.UsageOK, device.Usage = true, usage
	} else if device.Message == "" {
		device.Message = "fleet token could not read usage"
	}
	return device
}

func fetchPeerJSON(ctx context.Context, client *http.Client, target, token string) (object, error) {
	requestCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(requestCtx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("remote returned HTTP %d", response.StatusCode)
	}
	var value object
	if err := json.NewDecoder(io.LimitReader(response.Body, 8<<20)).Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func (a *app) discoveredDevices(ctx context.Context) []peerDevice {
	if a.peers == nil {
		return []peerDevice{}
	}
	return a.peers.snapshot(ctx, a.netTailscale(ctx), a.cfg.FleetToken)
}

func (a *app) netTailscale(ctx context.Context) tailscaleStatus {
	path, err := exec.LookPath("tailscale")
	if err != nil {
		return tailscaleStatus{Peers: []netNode{}}
	}
	output, err := commandWithTimeout(ctx, 4*time.Second, path, "status", "--json")
	if err != nil {
		return tailscaleStatus{Peers: []netNode{}}
	}
	status, err := parseTailscaleStatus(output)
	if err != nil {
		return tailscaleStatus{Peers: []netNode{}}
	}
	return status
}

func (a *app) netPeersAPI(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		jsonReply(w, http.StatusMethodNotAllowed, object{"error": "Use GET to read tailnet peers"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	jsonReply(w, http.StatusOK, a.discoveredDevices(ctx))
}
