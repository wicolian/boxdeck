package main

import "strings"

// pricing is expressed in USD per million tokens, list price, taken from the providers' own
// pricing pages on 2026-09-17 (platform.claude.com/docs/en/about-claude/pricing and
// developers.openai.com/api/docs/pricing). Cache write is the 5 minute write rate.
type pricing struct {
	In         float64 `json:"in"`
	CachedIn   float64 `json:"cachedIn"`
	CacheWrite float64 `json:"cacheWrite"`
	Out        float64 `json:"out"`
	Set        bool    `json:"-"`
}

type pricingConfig struct {
	Models map[string]pricing `json:"model"`
	// Tiers multiplies the whole estimate for a Codex service tier: flex 0.5, fast 2, priority 2.
	Tiers map[string]float64 `json:"tiers"`
}

// longContextTokens is the OpenAI threshold above which a turn is billed at the long context rate.
const longContextTokens = 272_000

func defaultPricing() map[string]pricing {
	return map[string]pricing{
		// Claude, per platform.claude.com. Cache read is 0.1x input (0.025x on Fable 5.1).
		"claude-fable-5-1":          {In: 10, CachedIn: 0.25, CacheWrite: 12.5, Out: 50, Set: true},
		"claude-fable-5":            {In: 10, CachedIn: 1, CacheWrite: 12.5, Out: 50, Set: true},
		"claude-opus-5":             {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 25, Set: true},
		"claude-opus-4-8":           {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 25, Set: true},
		"claude-opus-4-7":           {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 25, Set: true},
		"claude-opus-4-6":           {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 25, Set: true},
		"claude-opus-4-5":           {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 25, Set: true},
		"claude-sonnet-5":           {In: 2, CachedIn: 0.2, CacheWrite: 2.5, Out: 10, Set: true},
		"claude-sonnet-4-6":         {In: 3, CachedIn: 0.3, CacheWrite: 3.75, Out: 15, Set: true},
		"claude-sonnet-4-5":         {In: 3, CachedIn: 0.3, CacheWrite: 3.75, Out: 15, Set: true},
		"claude-haiku-4-5":          {In: 1, CachedIn: 0.1, CacheWrite: 1.25, Out: 5, Set: true},
		"claude-haiku-4-5-20251001": {In: 1, CachedIn: 0.1, CacheWrite: 1.25, Out: 5, Set: true},
		// OpenAI, per developers.openai.com, standard tier, short context. Cache write is 1.25x input.
		"gpt-6-astra":   {In: 10, CachedIn: 1, CacheWrite: 12.5, Out: 50, Set: true},
		"gpt-5.6-sol":   {In: 4, CachedIn: 0.4, CacheWrite: 5, Out: 20, Set: true},
		"gpt-5.6-terra": {In: 2, CachedIn: 0.2, CacheWrite: 2.5, Out: 12, Set: true},
		"gpt-5.6-luna":  {In: 0.2, CachedIn: 0.02, CacheWrite: 0.25, Out: 1.2, Set: true},
		"gpt-5.5":       {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 30, Set: true},
		"gpt-5.4":       {In: 2.5, CachedIn: 0.25, CacheWrite: 3.125, Out: 15, Set: true},
		"gpt-5.2":       {In: 1.25, CachedIn: 0.125, CacheWrite: 1.5625, Out: 10, Set: true},
		"gpt-5.1":       {In: 1.25, CachedIn: 0.125, CacheWrite: 1.5625, Out: 10, Set: true},
		"gpt-5":         {In: 1.25, CachedIn: 0.125, CacheWrite: 1.5625, Out: 10, Set: true},
		// OpenAI long context rates (a turn with more than longContextTokens of input).
		"gpt-6-astra+long":   {In: 20, CachedIn: 2, CacheWrite: 25, Out: 75, Set: true},
		"gpt-5.6-sol+long":   {In: 8, CachedIn: 0.8, CacheWrite: 10, Out: 30, Set: true},
		"gpt-5.6-terra+long": {In: 4, CachedIn: 0.4, CacheWrite: 5, Out: 18, Set: true},
		"gpt-5.6-luna+long":  {In: 0.4, CachedIn: 0.04, CacheWrite: 0.5, Out: 1.8, Set: true},
		"gpt-5.5+long":       {In: 10, CachedIn: 1, CacheWrite: 12.5, Out: 45, Set: true},
		"gpt-5.4+long":       {In: 5, CachedIn: 0.5, CacheWrite: 6.25, Out: 22.5, Set: true},
	}
}

func defaultTiers() map[string]float64 {
	return map[string]float64{"flex": 0.5, "fast": 2, "priority": 2, "default": 1}
}

// splitModelKey takes a ledger key like "gpt-6-astra@flex+long" and returns the base model,
// the service tier ("" when none) and whether the turn was long context.
func splitModelKey(key string) (model, tier string, long bool) {
	model = strings.TrimSpace(key)
	if strings.HasSuffix(model, "+long") {
		long = true
		model = strings.TrimSuffix(model, "+long")
	}
	if at := strings.LastIndex(model, "@"); at >= 0 {
		tier = model[at+1:]
		model = model[:at]
	}
	return model, tier, long
}

func pricingFor(key string, cfg pricingConfig) pricing {
	model, tier, long := splitModelKey(key)
	lookup := func(name string) (pricing, bool) {
		if cfg.Models != nil {
			if p, ok := cfg.Models[name]; ok {
				p.Set = true
				return p, true
			}
		}
		p, ok := defaultPricing()[name]
		return p, ok
	}
	p, ok := pricing{}, false
	if long {
		p, ok = lookup(model + "+long")
	}
	if !ok {
		p, ok = lookup(model)
	}
	if !ok {
		return pricing{}
	}
	mult := 1.0
	if tier != "" {
		tiers := defaultTiers()
		for k, v := range cfg.Tiers {
			tiers[k] = v
		}
		if m, found := tiers[tier]; found {
			mult = m
		}
	}
	return pricing{In: p.In * mult, CachedIn: p.CachedIn * mult, CacheWrite: p.CacheWrite * mult, Out: p.Out * mult, Set: p.Set}
}

func estimateCost(tokens tokenTotals, p pricing) float64 {
	return (float64(tokens.In)*p.In + float64(tokens.CachedIn)*p.CachedIn + float64(tokens.CacheWrite)*p.CacheWrite + float64(tokens.Out)*p.Out) / 1_000_000
}
