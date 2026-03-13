package llm

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

type pricingTier struct {
	PromptTokenThreshold int // inclusive upper bound (e.g., 200_000)
	InputPer1M           float64
	OutputPer1M          float64
	CachedInputPer1M     float64
}

type modelPricing struct {
	Provider string
	Model    string
	Tiers    []pricingTier
	// Cache storage price in USD per 1M tokens per hour.
	CacheStoragePer1MTokenHour float64
}

// UsageLedger aggregates token and cost usage across a run.
type UsageLedger struct {
	mu sync.Mutex

	provider string
	model    string
	pricing  modelPricing

	// GenerateContent totals
	generateCalls int
	inputTokens   int
	outputTokens  int
	cachedTokens  int
	toolTokens    int
	thoughtTokens int

	// Explicit cache totals (Gemini)
	cacheCreates          int
	cacheDeletes          int
	cacheStoredTokens     int
	cacheStorageHours     float64
	cacheStorageCostUSD   float64
	cacheReadCostUSD      float64
	nonCachedInputCostUSD float64
	outputCostUSD         float64

	// For visibility
	unknownPricing bool
}

func (l *UsageLedger) recordGenerate(resp GenerateResponse) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.generateCalls++
	l.inputTokens += resp.InputTokens
	l.outputTokens += resp.OutputTokens
	l.cachedTokens += resp.CachedTokens
	l.toolTokens += resp.ToolUsePromptTokens
	l.thoughtTokens += resp.ThoughtsTokens

	// Cost attribution:
	// - Cached tokens are billed at cached-input rate (Gemini explicit/implicit caching).
	// - Remaining prompt tokens are billed at standard input rate.
	// - Output tokens billed at output rate (thinking tokens are included in output for Gemini).
	//
	// Note: Some providers report tool/thought tokens separately; we keep them visible but
	// base cost on InputTokens/OutputTokens/CachedTokens to avoid double-counting.
	if l.unknownPricing {
		return
	}

	threshold := resp.InputTokens
	tier := selectTier(l.pricing.Tiers, threshold)
	if tier == nil {
		l.unknownPricing = true
		return
	}

	cached := clampNonNegative(resp.CachedTokens)
	nonCachedInput := resp.InputTokens - cached
	if nonCachedInput < 0 {
		nonCachedInput = 0
	}

	l.cacheReadCostUSD += (float64(cached) / 1_000_000.0) * tier.CachedInputPer1M
	l.nonCachedInputCostUSD += (float64(nonCachedInput) / 1_000_000.0) * tier.InputPer1M
	l.outputCostUSD += (float64(resp.OutputTokens) / 1_000_000.0) * tier.OutputPer1M
}

func (l *UsageLedger) recordCacheCreate(cachedTokens int, ttl time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.cacheCreates++
	if cachedTokens > 0 {
		l.cacheStoredTokens += cachedTokens
	}

	// Storage cost depends on token count and storage duration.
	if l.unknownPricing || l.pricing.CacheStoragePer1MTokenHour <= 0 {
		return
	}
	hours := ttl.Hours()
	if hours < 0 {
		hours = 0
	}
	l.cacheStorageHours += hours
	l.cacheStorageCostUSD += (float64(cachedTokens) / 1_000_000.0) * hours * l.pricing.CacheStoragePer1MTokenHour
}

func (l *UsageLedger) recordCacheDelete() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.cacheDeletes++
}

func (l *UsageLedger) TotalsUSD() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.cacheStorageCostUSD + l.cacheReadCostUSD + l.nonCachedInputCostUSD + l.outputCostUSD
}

func (l *UsageLedger) PrintSummary(prefix string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if prefix == "" {
		prefix = "💰 [LLM Cost]"
	}

	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("%s Review cost summary\n", prefix)
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
	fmt.Printf("Provider/Model: %s / %s\n", safeStr(l.provider, "unknown"), safeStr(l.model, "unknown"))
	fmt.Printf("Generate calls: %d\n", l.generateCalls)
	fmt.Printf("Tokens: input=%d (cached=%d), output=%d, tool=%d, thoughts=%d\n",
		l.inputTokens, l.cachedTokens, l.outputTokens, l.toolTokens, l.thoughtTokens)

	if l.unknownPricing {
		fmt.Println("Cost: unavailable (no pricing configured for this provider/model)")
		fmt.Println("Tip: set env vars to override pricing (see `LLM_COST_*`).")
		fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
		return
	}

	fmt.Printf("Cost (USD): $%.6f total\n", l.cacheStorageCostUSD+l.cacheReadCostUSD+l.nonCachedInputCostUSD+l.outputCostUSD)
	fmt.Printf("  - Input (non-cached): $%.6f\n", l.nonCachedInputCostUSD)
	fmt.Printf("  - Cached input reads: $%.6f\n", l.cacheReadCostUSD)
	fmt.Printf("  - Output:             $%.6f\n", l.outputCostUSD)
	if l.pricing.CacheStoragePer1MTokenHour > 0 {
		fmt.Printf("  - Cache storage:      $%.6f (creates=%d, storedTokens=%d, ttlHoursSum=%.3f)\n",
			l.cacheStorageCostUSD, l.cacheCreates, l.cacheStoredTokens, l.cacheStorageHours)
	} else if l.cacheCreates > 0 {
		fmt.Printf("  - Cache storage:      $0.000000 (storage pricing not configured)\n")
	}
	fmt.Println("═══════════════════════════════════════════════════════════════════════════════")
}

type CostTrackingProvider struct {
	inner  LLMProvider
	ledger *UsageLedger
}

// WrapWithCostTracking instruments an existing provider and returns:
// - an LLMProvider that records actual token usage for every LLM call
// - a ledger that can print an end-of-run cost summary
func WrapWithCostTracking(inner LLMProvider) (LLMProvider, *UsageLedger) {
	providerName, modelName := inferProviderAndModel(inner)
	pr := defaultPricing(providerName, modelName)
	pr = applyEnvOverrides(pr)

	ledger := &UsageLedger{
		provider:       providerName,
		model:          modelName,
		pricing:        pr,
		unknownPricing: len(pr.Tiers) == 0 || pr.Tiers[0].InputPer1M <= 0 || pr.Tiers[0].OutputPer1M <= 0,
	}

	return &CostTrackingProvider{inner: inner, ledger: ledger}, ledger
}

func (p *CostTrackingProvider) GenerateContent(ctx context.Context, req GenerateRequest) (GenerateResponse, error) {
	resp, err := p.inner.GenerateContent(ctx, req)
	if err == nil {
		p.ledger.recordGenerate(resp)
	}
	return resp, err
}

func (p *CostTrackingProvider) CreateCache(ctx context.Context, systemPrompt, staticContext string, tools []ToolDeclaration) (string, error) {
	cacheID, err := p.inner.CreateCache(ctx, systemPrompt, staticContext, tools)
	if err != nil || cacheID == "" {
		return cacheID, err
	}

	// Best-effort: only Gemini explicit caching returns token metadata on create.
	if gp, ok := p.inner.(*GeminiProvider); ok && gp != nil && gp.client != nil {
		if cache, getErr := gp.client.Caches.Get(ctx, cacheID, nil); getErr == nil && cache != nil && cache.UsageMetadata != nil {
			ttl := cache.ExpireTime.Sub(cache.CreateTime)
			p.ledger.recordCacheCreate(int(cache.UsageMetadata.TotalTokenCount), ttl)
		} else {
			// Still count the create; tokens/storage cost may be unknown.
			p.ledger.recordCacheCreate(0, 0)
		}
	} else {
		p.ledger.recordCacheCreate(0, 0)
	}

	return cacheID, err
}

func (p *CostTrackingProvider) DeleteCache(ctx context.Context, cacheID string) error {
	err := p.inner.DeleteCache(ctx, cacheID)
	if err == nil && cacheID != "" {
		p.ledger.recordCacheDelete()
	}
	return err
}

func inferProviderAndModel(inner LLMProvider) (providerName, model string) {
	switch v := inner.(type) {
	case *GeminiProvider:
		return "gemini", v.model
	case *OpenAIProvider:
		return "openai", v.model
	case *ClaudeProvider:
		return "claude", v.model
	default:
		return "unknown", ""
	}
}

func defaultPricing(providerName, model string) modelPricing {
	// Defaults are based on official provider pricing pages as of 2026-03.
	// Gemini prices: https://ai.google.dev/gemini-api/docs/pricing
	// OpenAI prices vary by model; gpt-4o is not listed on openai.com/api/pricing.
	// Anthropic prices vary by model; Claude 3.5 Sonnet pricing may be legacy.

	// Normalize model names (Gemini SDK sometimes prefixes with "models/").
	model = strings.TrimPrefix(model, "models/")

	switch providerName {
	case "gemini":
		switch model {
		case "gemini-3.1-pro-preview", "gemini-3.1-pro-preview-customtools":
			return modelPricing{
				Provider: providerName,
				Model:    model,
				Tiers: []pricingTier{
					{PromptTokenThreshold: 200_000, InputPer1M: 2.00, OutputPer1M: 12.00, CachedInputPer1M: 0.20},
					{PromptTokenThreshold: int(^uint(0) >> 1), InputPer1M: 4.00, OutputPer1M: 18.00, CachedInputPer1M: 0.40},
				},
				CacheStoragePer1MTokenHour: 4.50,
			}
		case "gemini-2.5-pro":
			return modelPricing{
				Provider: providerName,
				Model:    model,
				Tiers: []pricingTier{
					{PromptTokenThreshold: 200_000, InputPer1M: 1.25, OutputPer1M: 10.00, CachedInputPer1M: 0.125},
					{PromptTokenThreshold: int(^uint(0) >> 1), InputPer1M: 2.50, OutputPer1M: 15.00, CachedInputPer1M: 0.25},
				},
				CacheStoragePer1MTokenHour: 4.50,
			}
		case "gemini-2.5-flash":
			return modelPricing{
				Provider: providerName,
				Model:    model,
				Tiers: []pricingTier{
					// Pricing differs by modality; this tool is text-only, so we use text rates.
					{PromptTokenThreshold: int(^uint(0) >> 1), InputPer1M: 0.30, OutputPer1M: 2.50, CachedInputPer1M: 0.03},
				},
				CacheStoragePer1MTokenHour: 1.00,
			}
		default:
			// Unknown Gemini model: no pricing by default.
			return modelPricing{Provider: providerName, Model: model}
		}
	case "openai":
		if model == "gpt-4o" {
			// Commonly cited public pricing (may change; override via env vars if needed).
			return modelPricing{
				Provider: providerName,
				Model:    model,
				Tiers: []pricingTier{
					{PromptTokenThreshold: int(^uint(0) >> 1), InputPer1M: 2.50, OutputPer1M: 10.00, CachedInputPer1M: 0},
				},
			}
		}
	case "claude":
		// Claude 3.5 Sonnet legacy default (override recommended if you care about exact billing).
		if strings.Contains(strings.ToLower(model), "claude-3-5-sonnet") || strings.Contains(model, "3.5") {
			return modelPricing{
				Provider: providerName,
				Model:    model,
				Tiers: []pricingTier{
					{PromptTokenThreshold: 200_000, InputPer1M: 3.00, OutputPer1M: 15.00, CachedInputPer1M: 0},
					{PromptTokenThreshold: int(^uint(0) >> 1), InputPer1M: 6.00, OutputPer1M: 22.50, CachedInputPer1M: 0},
				},
			}
		}
	}

	return modelPricing{Provider: providerName, Model: model}
}

// Env overrides (simple, explicit; keeps repo deterministic without pulling live pricing).
//
// - LLM_COST_INPUT_PER_1M
// - LLM_COST_OUTPUT_PER_1M
// - LLM_COST_CACHED_INPUT_PER_1M
// - LLM_COST_CACHE_STORAGE_PER_1M_TOKEN_HOUR
//
// Overrides apply to the first tier only.
func applyEnvOverrides(p modelPricing) modelPricing {
	if len(p.Tiers) == 0 {
		return p
	}
	t := &p.Tiers[0]

	if v := parseEnvFloat("LLM_COST_INPUT_PER_1M"); v != nil {
		t.InputPer1M = *v
	}
	if v := parseEnvFloat("LLM_COST_OUTPUT_PER_1M"); v != nil {
		t.OutputPer1M = *v
	}
	if v := parseEnvFloat("LLM_COST_CACHED_INPUT_PER_1M"); v != nil {
		t.CachedInputPer1M = *v
	}
	if v := parseEnvFloat("LLM_COST_CACHE_STORAGE_PER_1M_TOKEN_HOUR"); v != nil {
		p.CacheStoragePer1MTokenHour = *v
	}
	return p
}

func parseEnvFloat(key string) *float64 {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil
	}
	f, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil
	}
	return &f
}

func selectTier(tiers []pricingTier, promptTokens int) *pricingTier {
	if len(tiers) == 0 {
		return nil
	}
	for i := range tiers {
		if promptTokens <= tiers[i].PromptTokenThreshold {
			return &tiers[i]
		}
	}
	return &tiers[len(tiers)-1]
}

func clampNonNegative(v int) int {
	if v < 0 {
		return 0
	}
	return v
}

func safeStr(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}
