package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type usageCLIArgs struct {
	Days  int
	Table bool
}

func parseUsageArgs(argv []string) (usageCLIArgs, error) {
	args := usageCLIArgs{Days: 30}
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--json":
			args.Table = false
		case "--table":
			args.Table = true
		case "--days":
			if i+1 >= len(argv) {
				return usageCLIArgs{}, fmt.Errorf("--days needs a value")
			}
			i++
			days, err := strconv.Atoi(argv[i])
			if err != nil || days < 1 || days > 30 {
				return usageCLIArgs{}, fmt.Errorf("days must be from 1 to 30")
			}
			args.Days = days
		default:
			return usageCLIArgs{}, fmt.Errorf("usage: boxdeck usage [--days N] [--json|--table]")
		}
	}
	return args, nil
}

func usageCommand(argv []string) error {
	args, err := parseUsageArgs(argv)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	now := time.Now()
	pricing := readPricingOverride(configPath(home))
	store, err := scanUsageRoots(filepath.Join(home, ".claude"), codexRoot(home), pricing, now)
	if err != nil {
		return err
	}
	response := snapshotFromStore(store, args.Days, now)
	if args.Table {
		return printUsageTable(response)
	}
	return json.NewEncoder(os.Stdout).Encode(response)
}

func readPricingOverride(path string) pricingConfig {
	b, err := os.ReadFile(path)
	if err != nil {
		return pricingConfig{}
	}
	var raw struct {
		Pricing pricingConfig `json:"pricing"`
	}
	if json.Unmarshal(b, &raw) != nil {
		return pricingConfig{}
	}
	return raw.Pricing
}

func printUsageTable(response usageResponse) error {
	fmt.Println("PROVIDER\tTODAY TOKENS\tTODAY COST\tSESSIONS")
	for _, provider := range []string{"claude", "codex"} {
		value := response.Providers[provider]
		fmt.Printf("%s\t%d\t$%.4f\t%d\n", provider, totalTokens(value.Today.Tokens), value.Today.CostUSD, value.Sessions)
	}
	fmt.Println("estimate at list price")
	return nil
}

func totalTokens(tokens tokenTotals) int64 {
	return tokens.In + tokens.CachedIn + tokens.CacheWrite + tokens.Out
}
