package barclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"time"
)

func ReadLocalUsage(ctx context.Context) (UsageResponse, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, "boxdeck", "usage", "--json")
	data, err := command.Output()
	if err != nil {
		return UsageResponse{}, err
	}
	var usage UsageResponse
	if err := json.Unmarshal(data, &usage); err != nil {
		return UsageResponse{}, fmt.Errorf("decode local usage: %w", err)
	}
	return usage, nil
}
