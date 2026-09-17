package main

import (
	"testing"

	"github.com/wicolian/boxdeck/internal/barclient"
)

func TestRetainFailureStartAcrossRefresh(t *testing.T) {
	old := map[string]barclient.BoxSnapshot{
		"http://down:8100": {URL: "http://down:8100", Since: "2026-09-17T22:05:00Z"},
	}
	boxes := []barclient.BoxSnapshot{{Name: "down", URL: "http://down:8100", Since: "2026-09-17T22:10:00Z"}}
	retainFailureStart(boxes, old)
	if boxes[0].Since != "2026-09-17T22:05:00Z" {
		t.Fatalf("since = %q", boxes[0].Since)
	}
}
