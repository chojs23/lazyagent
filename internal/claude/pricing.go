package claude

import "strings"

// ModelCosts holds per-token pricing for a model.
type ModelCosts struct {
	InputCostPerToken      float64
	OutputCostPerToken     float64
	CacheWriteCostPerToken float64
	CacheReadCostPerToken  float64
}

var pricing = map[string]ModelCosts{
	"claude-opus-4-7":   {5e-6, 25e-6, 6.25e-6, 0.5e-6},
	"claude-opus-4-6":   {5e-6, 25e-6, 6.25e-6, 0.5e-6},
	"claude-opus-4-5":   {5e-6, 25e-6, 6.25e-6, 0.5e-6},
	"claude-opus-4-1":   {15e-6, 75e-6, 18.75e-6, 1.5e-6},
	"claude-opus-4":     {15e-6, 75e-6, 18.75e-6, 1.5e-6},
	"claude-sonnet-4-6": {3e-6, 15e-6, 3.75e-6, 0.3e-6},
	"claude-sonnet-4-5": {3e-6, 15e-6, 3.75e-6, 0.3e-6},
	"claude-sonnet-4":   {3e-6, 15e-6, 3.75e-6, 0.3e-6},
	"claude-3-7-sonnet": {3e-6, 15e-6, 3.75e-6, 0.3e-6},
	"claude-3-5-sonnet": {3e-6, 15e-6, 3.75e-6, 0.3e-6},
	"claude-haiku-4-5":  {1e-6, 5e-6, 1.25e-6, 0.1e-6},
	"claude-3-5-haiku":  {0.8e-6, 4e-6, 1e-6, 0.08e-6},
	// OpenAI GPT-4 series
	"gpt-4o":      {2.5e-6, 10e-6, 2.5e-6, 1.25e-6},
	"gpt-4o-mini": {0.15e-6, 0.6e-6, 0.15e-6, 0.075e-6},
	"gpt-4.1":      {2e-6, 8e-6, 2e-6, 0.5e-6},
	"gpt-4.1-mini": {0.2e-6, 0.8e-6, 0.2e-6, 0.1e-6},
	"gpt-4.1-nano": {0.05e-6, 0.2e-6, 0.05e-6, 0.025e-6},
	// OpenAI GPT-5 series (base models)
	"gpt-5.4":      {2.5e-6, 15e-6, 2.5e-6, 0.25e-6},
	"gpt-5.4-mini": {0.75e-6, 4.5e-6, 0.75e-6, 0.075e-6},
	"gpt-5.4-nano": {0.2e-6, 1.25e-6, 0.2e-6, 0.02e-6},
	"gpt-5.3-codex": {1.75e-6, 14e-6, 1.75e-6, 0.175e-6},
	"gpt-5.3":       {1.75e-6, 14e-6, 1.75e-6, 0.175e-6},
	"gpt-5.2-codex": {1.75e-6, 14e-6, 1.75e-6, 0.175e-6},
	"gpt-5.2":       {0.875e-6, 7e-6, 0.875e-6, 0.175e-6},
	"gpt-5.1-codex-max":  {1.25e-6, 10e-6, 1.25e-6, 0.125e-6},
	"gpt-5.1-codex-mini": {0.25e-6, 2e-6, 0.25e-6, 0.025e-6},
	"gpt-5.1-codex":      {1.25e-6, 10e-6, 1.25e-6, 0.125e-6},
	"gpt-5.1":            {0.625e-6, 5e-6, 0.625e-6, 0.125e-6},
	"gpt-5-codex":        {1.25e-6, 10e-6, 1.25e-6, 0.125e-6},
	"gpt-5":              {0.625e-6, 5e-6, 0.625e-6, 0.125e-6},
	// OpenAI reasoning models
	"o3":      {2e-6, 8e-6, 2e-6, 0.5e-6},
	"o3-mini": {1.1e-6, 4.4e-6, 1.1e-6, 0.55e-6},
	"o4-mini": {1.1e-6, 4.4e-6, 1.1e-6, 0.275e-6},
	// Google
	"gemini-2.5-pro": {1.25e-6, 10e-6, 1.25e-6, 0.3125e-6},
}

// LookupCost finds pricing for a model name. It tries exact match,
// then prefix match, then substring match.
func LookupCost(model string) (ModelCosts, bool) {
	model = normalizeModel(model)

	if c, ok := pricing[model]; ok {
		return c, true
	}
	for name, c := range pricing {
		if strings.HasPrefix(model, name) {
			return c, true
		}
	}
	for name, c := range pricing {
		if strings.Contains(model, name) {
			return c, true
		}
	}
	return ModelCosts{}, false
}

// CalculateCost computes USD cost for given token usage and model.
func CalculateCost(model string, tokens TokenUsage) float64 {
	costs, ok := LookupCost(model)
	if !ok {
		return 0
	}
	return float64(tokens.InputTokens)*costs.InputCostPerToken +
		float64(tokens.OutputTokens)*costs.OutputCostPerToken +
		float64(tokens.CacheCreationTokens)*costs.CacheWriteCostPerToken +
		float64(tokens.CacheReadTokens)*costs.CacheReadCostPerToken
}

func normalizeModel(model string) string {
	// Strip date suffix like -20250514.
	parts := strings.Split(model, "-")
	if len(parts) > 1 {
		last := parts[len(parts)-1]
		if len(last) == 8 && isDigits(last) {
			parts = parts[:len(parts)-1]
		}
	}
	return strings.Join(parts, "-")
}
