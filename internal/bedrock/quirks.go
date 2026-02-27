package bedrock

import "strings"

// ── Model Detection Helpers ──

// isClaude45Sampling returns true for Claude Sonnet 4.5 and Haiku 4.5,
// which cannot accept both temperature AND topP simultaneously.
func isClaude45Sampling(modelID string) bool {
	m := strings.ToLower(modelID)
	return strings.Contains(m, "claude-sonnet-4-5") ||
		strings.Contains(m, "claude-haiku-4-5")
}

// ── Prompt Caching Quirks ──

// cacheMinTokens returns the minimum tokens per cache checkpoint for a model.
func cacheMinTokens(modelID string) int {
	m := strings.ToLower(modelID)
	switch {
	case strings.Contains(m, "claude-opus-4-5"),
		strings.Contains(m, "claude-haiku-4-5"):
		return 4096
	case strings.Contains(m, "claude-3-5-haiku"):
		return 2048
	case strings.Contains(m, "claude"):
		return 1024 // Claude 3.7, Sonnet 4, Opus 4, etc.
	case strings.Contains(m, "nova"):
		return 1000
	default:
		return 1024
	}
}

// supportsExtendedTTL returns true for models that support 1-hour cache TTL.
func supportsExtendedTTL(modelID string) bool {
	m := strings.ToLower(modelID)
	return strings.Contains(m, "claude-opus-4-5") ||
		strings.Contains(m, "claude-haiku-4-5") ||
		strings.Contains(m, "claude-sonnet-4-5")
}

// supportsToolCaching returns true if the model supports cache points in tools.
func supportsToolCaching(modelID string) bool {
	// Claude models support system, messages, and tools caching.
	// Nova models only support system and messages.
	return strings.Contains(strings.ToLower(modelID), "claude")
}
