package main

import "testing"

func TestParseBrowserPortFromDevToolsStderr(t *testing.T) {
	got, ok := parseBrowserPort([]byte("[123] DevTools listening on ws://127.0.0.1:45678/devtools/browser/abc\n"))
	if !ok || got != 45678 {
		t.Fatalf("parseBrowserPort() = %d, %v; want 45678, true", got, ok)
	}
}

func TestRewriteCDPURLs(t *testing.T) {
	input := []byte(`{"webSocketDebuggerUrl":"ws://127.0.0.1:9222/devtools/browser/abc","nested":{"webSocketDebuggerUrl":"ws://localhost:9222/devtools/page/def"}}`)
	got, err := rewriteCDPJSON(input, "https", "box.example/cdp")
	if err != nil {
		t.Fatal(err)
	}
	want := `"wss://box.example/cdp/devtools/browser/abc"`
	if !containsString(got, want) || !containsString(got, `"wss://box.example/cdp/devtools/page/def"`) {
		t.Fatalf("rewritten JSON = %s", got)
	}
}

func containsString(b []byte, want string) bool {
	for i := 0; i+len(want) <= len(b); i++ {
		if string(b[i:i+len(want)]) == want {
			return true
		}
	}
	return false
}
