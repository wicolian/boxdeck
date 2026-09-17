package main

import "testing"

func TestStripANSIAndTailPaneText(t *testing.T) {
	input := "\x1b[32mworking\x1b[0m\r\nsecond\nthird\nfourth\n"
	got := tailPaneText(input, 2)
	if got != "third\nfourth" {
		t.Fatalf("tailPaneText() = %q, want last two clean lines", got)
	}
}

func TestPaneNeedsInput(t *testing.T) {
	for _, text := range []string{"Continue? (y/n)", "Press enter to continue", "Trust this folder?"} {
		if !paneNeedsInput(text) {
			t.Errorf("paneNeedsInput(%q) = false", text)
		}
	}
	if paneNeedsInput("working normally") {
		t.Fatal("ordinary output was marked as input")
	}
}
