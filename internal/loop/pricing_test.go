package loop

import (
	"math"
	"testing"
)

func TestPricingForModel(t *testing.T) {
	tests := []struct {
		model   string
		wantOK  bool
		wantFam string // which family's input price we expect (for a spot check)
		wantIn  float64
	}{
		{"claude-opus-4-8", true, "opus", 15.0 / 1_000_000},
		{"claude-sonnet-5", true, "sonnet", 3.0 / 1_000_000},
		{"claude-haiku-4-5", true, "haiku", 1.0 / 1_000_000},
		{"CLAUDE-OPUS-4-8", true, "opus", 15.0 / 1_000_000}, // case-insensitive
		{"gpt-4o", false, "", 0},
		{"", false, "", 0},
	}
	for _, tt := range tests {
		p, ok := pricingForModel(tt.model)
		if ok != tt.wantOK {
			t.Errorf("pricingForModel(%q) ok = %v, want %v", tt.model, ok, tt.wantOK)
			continue
		}
		if ok && p.input != tt.wantIn {
			t.Errorf("pricingForModel(%q) input = %v, want %v (%s)", tt.model, p.input, tt.wantIn, tt.wantFam)
		}
	}
}

func TestCostForUsage(t *testing.T) {
	t.Run("nil usage is zero", func(t *testing.T) {
		if got := costForUsage("claude-opus-4-8", nil); got != 0 {
			t.Errorf("costForUsage(nil) = %v, want 0", got)
		}
	})

	t.Run("unknown model is zero", func(t *testing.T) {
		u := &usageInfo{InputTokens: 1000, OutputTokens: 1000}
		if got := costForUsage("gpt-4o", u); got != 0 {
			t.Errorf("costForUsage(unknown) = %v, want 0", got)
		}
	})

	t.Run("sonnet cost sums all four categories", func(t *testing.T) {
		u := &usageInfo{
			InputTokens:              1000,  // 1000 * 3/M    = 0.003
			OutputTokens:             500,   // 500  * 15/M   = 0.0075
			CacheCreationInputTokens: 200,   // 200  * 3.75/M = 0.00075
			CacheReadInputTokens:     10000, // 10000* 0.3/M  = 0.003
		}
		want := 0.003 + 0.0075 + 0.00075 + 0.003
		got := costForUsage("claude-sonnet-5", u)
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("costForUsage(sonnet) = %v, want %v", got, want)
		}
	})

	t.Run("zero usage is zero cost", func(t *testing.T) {
		if got := costForUsage("claude-opus-4-8", &usageInfo{}); got != 0 {
			t.Errorf("costForUsage(zero usage) = %v, want 0", got)
		}
	})
}
