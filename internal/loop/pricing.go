package loop

import "strings"

// modelPrice holds per-token USD prices for the four token categories Claude
// reports. Values are USD per single token (list price / 1_000_000).
type modelPrice struct {
	input      float64
	output     float64
	cacheWrite float64 // cache_creation_input_tokens (5-minute write)
	cacheRead  float64 // cache_read_input_tokens
}

// perMTok converts list prices given per million tokens into per-token prices.
func perMTok(input, output, cacheWrite, cacheRead float64) modelPrice {
	const m = 1_000_000
	return modelPrice{input / m, output / m, cacheWrite / m, cacheRead / m}
}

// modelPrices maps a coarse model family to its list prices (USD per MTok).
//
// These are hardcoded Anthropic list prices and may drift; they only affect the
// cost estimate shown in the TUI, never billing. Cache-write is priced at 1.25x
// input and cache-read at 0.1x input, per Anthropic's prompt-caching pricing.
// Update here if Anthropic changes list prices.
var modelPrices = map[string]modelPrice{
	"opus":   perMTok(15, 75, 18.75, 1.5),
	"sonnet": perMTok(3, 15, 3.75, 0.3),
	"haiku":  perMTok(1, 5, 1.25, 0.1),
}

// pricingForModel returns the price table for a model name (e.g.
// "claude-opus-4-8"), matching on the family substring. Returns false when the
// model is unknown, so callers can show token counts without a cost figure.
func pricingForModel(model string) (modelPrice, bool) {
	m := strings.ToLower(model)
	for family, price := range modelPrices {
		if strings.Contains(m, family) {
			return price, true
		}
	}
	return modelPrice{}, false
}

// costForUsage derives the USD cost of one assistant message from its token
// usage and model. Returns 0 for unknown models.
func costForUsage(model string, u *usageInfo) float64 {
	if u == nil {
		return 0
	}
	p, ok := pricingForModel(model)
	if !ok {
		return 0
	}
	return float64(u.InputTokens)*p.input +
		float64(u.OutputTokens)*p.output +
		float64(u.CacheCreationInputTokens)*p.cacheWrite +
		float64(u.CacheReadInputTokens)*p.cacheRead
}
