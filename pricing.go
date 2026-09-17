package main

import "strings"

// pricing is expressed in USD per million tokens.
type pricing struct {
	In         float64 `json:"in"`
	CachedIn   float64 `json:"cachedIn"`
	CacheWrite float64 `json:"cacheWrite"`
	Out        float64 `json:"out"`
	Set        bool    `json:"-"`
}

type pricingConfig struct {
	Models map[string]pricing `json:"model"`
}

func defaultPricing() map[string]pricing {
	return map[string]pricing{
		// The future Claude 5 names do not have a public list price in the
		// provider material available to this binary.
		"claude-opus-5":    {},
		"claude-sonnet-5":  {},
		"claude-haiku-4-5": {In: 1, CachedIn: 0.1, CacheWrite: 1.25, Out: 5, Set: true},
		"gpt-6-astra":      {In: 10, CachedIn: 1, CacheWrite: 12.5, Out: 50, Set: true},
		"gpt-5.6-terra":    {In: 2, CachedIn: 0.2, CacheWrite: 2.5, Out: 12, Set: true},
		"gpt-5.6-luna":     {In: 0.2, CachedIn: 0.02, CacheWrite: 0.25, Out: 1.2, Set: true},
		"gpt-5.6-sol":      {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 30, Set: true},
	}
}

func pricingFor(model string, cfg pricingConfig) pricing {
	model = strings.TrimSpace(model)
	if cfg.Models != nil {
		if p, ok := cfg.Models[model]; ok {
			p.Set = true
			return p
		}
	}
	if p, ok := defaultPricing()[model]; ok {
		return p
	}
	return pricing{}
}

func estimateCost(tokens tokenTotals, p pricing) float64 {
	return (float64(tokens.In)*p.In + float64(tokens.CachedIn)*p.CachedIn + float64(tokens.CacheWrite)*p.CacheWrite + float64(tokens.Out)*p.Out) / 1_000_000
}
