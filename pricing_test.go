package main

import "testing"

func TestPricingTiersAndLongContext(t *testing.T) {
	base := pricingFor("gpt-6-astra", pricingConfig{})
	flex := pricingFor("gpt-6-astra@flex", pricingConfig{})
	long := pricingFor("gpt-6-astra+long", pricingConfig{})
	fastLong := pricingFor("gpt-6-astra@fast+long", pricingConfig{})
	if base.In != 10 || flex.In != 5 || long.In != 20 || fastLong.In != 40 || fastLong.Out != 150 {
		t.Fatalf("astra prices: base %v flex %v long %v fastLong %v", base, flex, long, fastLong)
	}
	if p := pricingFor("claude-opus-5", pricingConfig{}); !p.Set || p.In != 5 || p.CachedIn != 0.5 || p.Out != 25 {
		t.Fatalf("opus 5 = %+v", p)
	}
	custom := pricingFor("gpt-5.6-luna@flex", pricingConfig{Tiers: map[string]float64{"flex": 0.25}})
	if custom.Out != 0.3 {
		t.Fatalf("custom flex multiplier: %v", custom.Out)
	}
}
