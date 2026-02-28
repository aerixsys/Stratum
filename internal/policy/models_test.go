package policy

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestLoadDefaultModelPolicy_ExplicitPath(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "model-policy.yaml")
	if err := os.WriteFile(path, []byte("version: 1\nexclude: [\"anthropic.*\"]\n"), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}

	p, err := LoadDefaultModelPolicy(path)
	if err != nil {
		t.Fatalf("LoadDefaultModelPolicy() error = %v", err)
	}
	if p.FilePath() != path {
		t.Fatalf("FilePath() = %q, want %q", p.FilePath(), path)
	}
	if !p.IsBlocked("anthropic.claude-3-sonnet") {
		t.Fatalf("expected wildcard block match")
	}
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

func TestResolveDefaultPolicyPath_ExplicitPriority(t *testing.T) {
	tmp := t.TempDir()
	explicitPath := filepath.Join(tmp, "explicit-model-policy.yaml")
	if err := os.WriteFile(explicitPath, []byte("version: 1\nexclude: []\n"), 0o600); err != nil {
		t.Fatalf("write explicit policy: %v", err)
	}

	execDir := filepath.Join(tmp, "exec")
	execPolicyPath := filepath.Join(execDir, "config", "model-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(execPolicyPath), 0o755); err != nil {
		t.Fatalf("mkdir exec config: %v", err)
	}
	if err := os.WriteFile(execPolicyPath, []byte("version: 1\nexclude: []\n"), 0o600); err != nil {
		t.Fatalf("write exec policy: %v", err)
	}

	cwdDir := filepath.Join(tmp, "cwd")
	cwdPolicyPath := filepath.Join(cwdDir, "config", "model-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(cwdPolicyPath), 0o755); err != nil {
		t.Fatalf("mkdir cwd config: %v", err)
	}
	if err := os.WriteFile(cwdPolicyPath, []byte("version: 1\nexclude: []\n"), 0o600); err != nil {
		t.Fatalf("write cwd policy: %v", err)
	}

	oldExec := getExecutablePath
	oldWD := getWorkingDir
	defer func() {
		getExecutablePath = oldExec
		getWorkingDir = oldWD
	}()
	getExecutablePath = func() (string, error) { return filepath.Join(execDir, "stratum"), nil }
	getWorkingDir = func() (string, error) { return cwdDir, nil }

	resolved, err := ResolveDefaultPolicyPath(explicitPath)
	if err != nil {
		t.Fatalf("ResolveDefaultPolicyPath() error = %v", err)
	}
	if resolved != explicitPath {
		t.Fatalf("resolved path = %q, want %q", resolved, explicitPath)
	}
}

func TestResolveDefaultPolicyPath_ExecutableFallback(t *testing.T) {
	tmp := t.TempDir()
	execDir := filepath.Join(tmp, "exec")
	execPolicyPath := filepath.Join(execDir, "config", "model-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(execPolicyPath), 0o755); err != nil {
		t.Fatalf("mkdir exec config: %v", err)
	}
	if err := os.WriteFile(execPolicyPath, []byte("version: 1\nexclude: []\n"), 0o600); err != nil {
		t.Fatalf("write exec policy: %v", err)
	}

	cwdDir := filepath.Join(tmp, "cwd")
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatalf("mkdir cwd dir: %v", err)
	}

	oldExec := getExecutablePath
	oldWD := getWorkingDir
	defer func() {
		getExecutablePath = oldExec
		getWorkingDir = oldWD
	}()
	getExecutablePath = func() (string, error) { return filepath.Join(execDir, "stratum"), nil }
	getWorkingDir = func() (string, error) { return cwdDir, nil }

	resolved, err := ResolveDefaultPolicyPath("")
	if err != nil {
		t.Fatalf("ResolveDefaultPolicyPath() error = %v", err)
	}
	if resolved != execPolicyPath {
		t.Fatalf("resolved path = %q, want %q", resolved, execPolicyPath)
	}
}

func TestResolveDefaultPolicyPath_CWDFallback(t *testing.T) {
	tmp := t.TempDir()
	cwdDir := filepath.Join(tmp, "cwd")
	cwdPolicyPath := filepath.Join(cwdDir, "config", "model-policy.yaml")
	if err := os.MkdirAll(filepath.Dir(cwdPolicyPath), 0o755); err != nil {
		t.Fatalf("mkdir cwd config: %v", err)
	}
	if err := os.WriteFile(cwdPolicyPath, []byte("version: 1\nexclude: []\n"), 0o600); err != nil {
		t.Fatalf("write cwd policy: %v", err)
	}

	oldExec := getExecutablePath
	oldWD := getWorkingDir
	defer func() {
		getExecutablePath = oldExec
		getWorkingDir = oldWD
	}()
	getExecutablePath = func() (string, error) { return "", os.ErrNotExist }
	getWorkingDir = func() (string, error) { return cwdDir, nil }

	resolved, err := ResolveDefaultPolicyPath("")
	if err != nil {
		t.Fatalf("ResolveDefaultPolicyPath() error = %v", err)
	}
	if resolved != cwdPolicyPath {
		t.Fatalf("resolved path = %q, want %q", resolved, cwdPolicyPath)
	}
}

func TestResolveDefaultPolicyPath_NotFound(t *testing.T) {
	tmp := t.TempDir()
	execDir := filepath.Join(tmp, "exec")
	cwdDir := filepath.Join(tmp, "cwd")
	if err := os.MkdirAll(execDir, 0o755); err != nil {
		t.Fatalf("mkdir exec dir: %v", err)
	}
	if err := os.MkdirAll(cwdDir, 0o755); err != nil {
		t.Fatalf("mkdir cwd dir: %v", err)
	}

	oldExec := getExecutablePath
	oldWD := getWorkingDir
	defer func() {
		getExecutablePath = oldExec
		getWorkingDir = oldWD
	}()
	getExecutablePath = func() (string, error) { return filepath.Join(execDir, "stratum"), nil }
	getWorkingDir = func() (string, error) { return cwdDir, nil }

	_, err := ResolveDefaultPolicyPath(filepath.Join(tmp, "missing.yaml"))
	if err == nil {
		t.Fatal("expected error")
	}
	if got := err.Error(); !strings.Contains(got, "tried:") {
		t.Fatalf("expected tried paths in error, got %q", got)
	}
}
