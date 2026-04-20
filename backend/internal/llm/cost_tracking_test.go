package llm

import (
	"strings"
	"testing"
)

func TestUsageLedgerMerge(t *testing.T) {
	main := &UsageLedger{
		provider:              "gemini",
		model:                 "gemini-3.1-pro-preview-customtools",
		generateCalls:         2,
		inputTokens:           100,
		outputTokens:          40,
		nonCachedInputCostUSD: 0.10,
		outputCostUSD:         0.20,
		pricing: modelPricing{
			CacheStoragePer1MTokenHour: 0,
		},
	}
	flash := &UsageLedger{
		provider:         "gemini",
		model:            "gemini-3-flash-preview",
		generateCalls:    1,
		inputTokens:      20,
		outputTokens:     10,
		cacheReadCostUSD: 0.01,
		outputCostUSD:    0.02,
		pricing: modelPricing{
			CacheStoragePer1MTokenHour: 1.25,
		},
	}

	main.Merge(flash)

	if main.generateCalls != 3 {
		t.Fatalf("expected merged generate calls to equal 3, got %d", main.generateCalls)
	}
	if main.inputTokens != 120 || main.outputTokens != 50 {
		t.Fatalf("unexpected merged tokens: input=%d output=%d", main.inputTokens, main.outputTokens)
	}
	if got := main.TotalsUSD(); got != 0.33 {
		t.Fatalf("expected merged total cost 0.33, got %.2f", got)
	}
	if !strings.Contains(main.model, "gemini-3.1-pro-preview-customtools") || !strings.Contains(main.model, "gemini-3-flash-preview") {
		t.Fatalf("expected merged model list to include both models, got %q", main.model)
	}
	if main.pricing.CacheStoragePer1MTokenHour != 1.25 {
		t.Fatalf("expected merged cache storage price 1.25, got %.2f", main.pricing.CacheStoragePer1MTokenHour)
	}
}

func TestDefaultPricingGemini3FlashPreview(t *testing.T) {
	pricing := defaultPricing("gemini", "gemini-3-flash-preview")

	if len(pricing.Tiers) != 1 {
		t.Fatalf("expected one pricing tier, got %d", len(pricing.Tiers))
	}
	tier := pricing.Tiers[0]
	if tier.InputPer1M != 0.50 || tier.OutputPer1M != 3.00 || tier.CachedInputPer1M != 0.05 {
		t.Fatalf("unexpected pricing tier: %+v", tier)
	}
	if pricing.CacheStoragePer1MTokenHour != 1.00 {
		t.Fatalf("expected cache storage price 1.00, got %.2f", pricing.CacheStoragePer1MTokenHour)
	}
}
