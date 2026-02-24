package bedrock

import "testing"

func TestModelFamily(t *testing.T) {
	tests := []struct {
		modelID  string
		expected string
	}{
		{"anthropic.claude-3-7-sonnet-20250219-v1:0", "claude"},
		{"amazon.nova-pro-v1:0", "nova"},
		{"mistral.mistral-large-2402-v1:0", "mistral"},
		{"meta.llama3-70b-instruct-v1:0", "llama"},
		{"some-unknown-model", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := modelFamily(tt.modelID)
			if got != tt.expected {
				t.Errorf("modelFamily(%s) = %s, want %s", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestIsClaude45Sampling(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		{"anthropic.claude-sonnet-4-5-20250929-v1:0", true},
		{"anthropic.claude-haiku-4-5-20251001-v1:0", true},
		{"anthropic.claude-3-7-sonnet-20250219-v1:0", false},
		{"anthropic.claude-sonnet-4-20250514-v1:0", false},
		{"amazon.nova-pro-v1:0", false},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := isClaude45Sampling(tt.modelID)
			if got != tt.expected {
				t.Errorf("isClaude45Sampling(%s) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestSupportsThinking(t *testing.T) {
	tests := []struct {
		modelID  string
		expected bool
	}{
		{"anthropic.claude-3-7-sonnet-20250219-v1:0", true},
		{"anthropic.claude-sonnet-4-20250514-v1:0", true},
		{"anthropic.claude-opus-4-20250514-v1:0", true},
		{"anthropic.claude-haiku-4-5-20251001-v1:0", true},
		{"anthropic.claude-3-5-sonnet-20241022-v2:0", false},
		{"amazon.nova-pro-v1:0", false},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := supportsThinking(tt.modelID)
			if got != tt.expected {
				t.Errorf("supportsThinking(%s) = %v, want %v", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestCacheMinTokens(t *testing.T) {
	tests := []struct {
		modelID  string
		expected int
	}{
		{"anthropic.claude-opus-4-5-20251101-v1:0", 4096},
		{"anthropic.claude-haiku-4-5-20251001-v1:0", 4096},
		{"anthropic.claude-3-5-haiku-20241022-v1:0", 2048},
		{"anthropic.claude-3-7-sonnet-20250219-v1:0", 1024},
		{"anthropic.claude-sonnet-4-20250514-v1:0", 1024},
		{"amazon.nova-pro-v1:0", 1000},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			got := cacheMinTokens(tt.modelID)
			if got != tt.expected {
				t.Errorf("cacheMinTokens(%s) = %d, want %d", tt.modelID, got, tt.expected)
			}
		})
	}
}

func TestSupportsPromptCaching(t *testing.T) {
	if !supportsPromptCaching("anthropic.claude-3-7-sonnet") {
		t.Error("Claude should support prompt caching")
	}
	if !supportsPromptCaching("amazon.nova-pro-v1:0") {
		t.Error("Nova should support prompt caching")
	}
	if supportsPromptCaching("mistral.mistral-large") {
		t.Error("Mistral should not support prompt caching")
	}
}

func TestSupportsToolCaching(t *testing.T) {
	if !supportsToolCaching("anthropic.claude-3-7-sonnet") {
		t.Error("Claude should support tool caching")
	}
	if supportsToolCaching("amazon.nova-pro-v1:0") {
		t.Error("Nova should not support tool caching")
	}
}

func TestAdaptiveThinkingAndExtendedTTL(t *testing.T) {
	if !isAdaptiveThinkingModel("anthropic.claude-opus-4-6-20260101-v1:0") {
		t.Fatal("expected adaptive thinking model")
	}
	if isAdaptiveThinkingModel("anthropic.claude-opus-4-20250514-v1:0") {
		t.Fatal("unexpected adaptive thinking support")
	}
	if !supportsExtendedTTL("anthropic.claude-sonnet-4-5-20250929-v1:0") {
		t.Fatal("expected extended ttl support")
	}
	if supportsExtendedTTL("anthropic.claude-3-7-sonnet-20250219-v1:0") {
		t.Fatal("unexpected extended ttl support")
	}
}
