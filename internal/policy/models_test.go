package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLoadModelPolicy_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "model-policy.yaml")
	if err := os.WriteFile(path, []byte(`
version: 1
exclude:
  - "  Anthropic.*  "
  - "anthropic.*"
  - "claude-*"
  - ""
`), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	p, err := LoadModelPolicy(path)
	if err != nil {
		t.Fatalf("LoadModelPolicy() error = %v", err)
	}

	want := []string{"anthropic.*", "claude-*"}
	if !reflect.DeepEqual(p.ExcludePatterns(), want) {
		t.Fatalf("ExcludePatterns() = %v, want %v", p.ExcludePatterns(), want)
	}
}

func TestLoadModelPolicy_Errors(t *testing.T) {
	t.Run("empty path", func(t *testing.T) {
		if _, err := LoadModelPolicy("   "); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "model-policy.yaml")
		if err := os.WriteFile(path, []byte("  \n"), 0o600); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		if _, err := LoadModelPolicy(path); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("unsupported version", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "model-policy.yaml")
		if err := os.WriteFile(path, []byte("version: 2\nexclude: [\"anthropic.*\"]\n"), 0o600); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		if _, err := LoadModelPolicy(path); err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("no valid patterns", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "model-policy.yaml")
		if err := os.WriteFile(path, []byte("version: 1\nexclude: [\"\", \"   \"]\n"), 0o600); err != nil {
			t.Fatalf("write policy: %v", err)
		}
		p, err := LoadModelPolicy(path)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(p.ExcludePatterns()) != 0 {
			t.Fatalf("expected empty exclude patterns, got %v", p.ExcludePatterns())
		}
	})
}

func TestModelPolicy_IsBlocked(t *testing.T) {
	p := &ModelPolicy{
		exclude: []string{
			"anthropic.*",
			"claude-*",
			"*-forbidden",
			"*blocked*",
			"exact-model",
		},
	}

	tests := []struct {
		modelID string
		blocked bool
	}{
		{modelID: "anthropic.claude-3-sonnet", blocked: true},
		{modelID: "CLAUDE-test", blocked: true},
		{modelID: "abc-forbidden", blocked: true},
		{modelID: "my-blocked-model", blocked: true},
		{modelID: "exact-model", blocked: true},
		{modelID: "amazon.nova-micro-v1:0", blocked: false},
		{modelID: "", blocked: false},
	}

	for _, tt := range tests {
		if got := p.IsBlocked(tt.modelID); got != tt.blocked {
			t.Fatalf("IsBlocked(%q) = %v, want %v", tt.modelID, got, tt.blocked)
		}
	}
}

func TestResolveDefaultPolicyPath(t *testing.T) {
	tmp := t.TempDir()
	repo := filepath.Join(tmp, "repo")
	if err := os.MkdirAll(filepath.Join(repo, "config"), 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(repo, "internal", "server"), 0o755); err != nil {
		t.Fatalf("mkdir nested: %v", err)
	}
	policyPath := filepath.Join(repo, "config", "model-policy.yaml")
	if err := os.WriteFile(policyPath, []byte("version: 1\nexclude: [\"anthropic.*\"]\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() { _ = os.Chdir(cwd) }()
	if err := os.Chdir(filepath.Join(repo, "internal", "server")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolved, err := ResolveDefaultPolicyPath()
	if err != nil {
		t.Fatalf("ResolveDefaultPolicyPath() error = %v", err)
	}
	if resolved != policyPath {
		t.Fatalf("resolved path = %q, want %q", resolved, policyPath)
	}
}
