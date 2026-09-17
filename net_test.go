package main

import "testing"

func TestParseTailscaleStatus(t *testing.T) {
	status := []byte(`{"Self":{"HostName":"box","DNSName":"box.tail.ts.net.","OS":"linux","TailscaleIPs":["100.64.0.5","fd7a:115c:a1e0::5"],"Online":true},"Peer":{"node1":{"HostName":"mac","OS":"darwin","TailscaleIPs":["100.64.0.6"],"Online":false}}}`)
	got, err := parseTailscaleStatus(status)
	if err != nil {
		t.Fatal(err)
	}
	if got.Self.Name != "box" || len(got.Self.IPs) != 2 || len(got.Peers) != 1 || got.Peers[0].Online {
		t.Fatalf("parsed tailscale status = %#v", got)
	}
}

func TestParseTailscaleServeStatus(t *testing.T) {
	serve := []byte(`{"Web":{"https://box.ts.net":{"Handlers":{"/":{"Proxy":"http://127.0.0.1:8100"}}}},"TCP":{"443":{"HTTPS":true,"Handler":"http://127.0.0.1:8100"}}}`)
	got, err := parseTailscaleServeStatus(serve)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Target == "" || got[1].Target == "" {
		t.Fatalf("parsed serve status = %#v", got)
	}
}
