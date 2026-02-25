package policy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/goccy/go-yaml"
)

const (
	supportedModelPolicyVersion = 1
	defaultPolicyRelativePath   = "config/model-policy.yaml"
)

type modelPolicyFile struct {
	Version int      `yaml:"version"`
	Exclude []string `yaml:"exclude"`
}

// ModelPolicy contains normalized model-exclusion patterns.
type ModelPolicy struct {
	version  int
	exclude  []string
	filePath string
}

// LoadDefaultModelPolicy loads config/model-policy.yaml by searching upward from the cwd.
func LoadDefaultModelPolicy() (*ModelPolicy, error) {
	path, err := ResolveDefaultPolicyPath()
	if err != nil {
		return nil, err
	}
	return LoadModelPolicy(path)
}

// ResolveDefaultPolicyPath finds config/model-policy.yaml by walking up from the cwd.
func ResolveDefaultPolicyPath() (string, error) {
	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolve cwd: %w", err)
	}

	cur := wd
	for {
		candidate := filepath.Join(cur, defaultPolicyRelativePath)
		info, statErr := os.Stat(candidate)
		if statErr == nil {
			if info.IsDir() {
				return "", fmt.Errorf("model policy path is a directory: %s", candidate)
			}
			return candidate, nil
		}
		if !os.IsNotExist(statErr) {
			return "", fmt.Errorf("stat model policy %s: %w", candidate, statErr)
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	return "", fmt.Errorf("model policy file %s not found", defaultPolicyRelativePath)
}

// LoadModelPolicy parses and validates model policy YAML at path.
func LoadModelPolicy(path string) (*ModelPolicy, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("model policy path is required")
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read model policy: %w", err)
	}
	if strings.TrimSpace(string(raw)) == "" {
		return nil, fmt.Errorf("model policy file is empty")
	}

	var cfg modelPolicyFile
	if err := yaml.Unmarshal(raw, &cfg); err != nil {
		return nil, fmt.Errorf("parse model policy: %w", err)
	}
	if cfg.Version != supportedModelPolicyVersion {
		return nil, fmt.Errorf("unsupported model policy version %d (expected %d)", cfg.Version, supportedModelPolicyVersion)
	}

	exclude := normalizePatterns(cfg.Exclude)

	return &ModelPolicy{
		version:  cfg.Version,
		exclude:  exclude,
		filePath: path,
	}, nil
}

// IsBlocked reports whether modelID matches any excluded pattern.
func (p *ModelPolicy) IsBlocked(modelID string) bool {
	if p == nil {
		return false
	}
	value := strings.ToLower(strings.TrimSpace(modelID))
	if value == "" {
		return false
	}
	for _, pattern := range p.exclude {
		if matchWildcard(pattern, value) {
			return true
		}
	}
	return false
}

// ExcludePatterns returns a copy of normalized exclusion patterns.
func (p *ModelPolicy) ExcludePatterns() []string {
	if p == nil || len(p.exclude) == 0 {
		return nil
	}
	out := make([]string, len(p.exclude))
	copy(out, p.exclude)
	return out
}

// FilePath returns the policy file path used during loading.
func (p *ModelPolicy) FilePath() string {
	if p == nil {
		return ""
	}
	return p.filePath
}

func normalizePatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(patterns))
	out := make([]string, 0, len(patterns))
	for _, item := range patterns {
		normalized := strings.ToLower(strings.TrimSpace(item))
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// matchWildcard performs case-insensitive wildcard matching where '*' matches any substring.
func matchWildcard(pattern, value string) bool {
	if pattern == "" {
		return false
	}

	if !strings.Contains(pattern, "*") {
		return pattern == value
	}

	parts := strings.Split(pattern, "*")

	if prefix := parts[0]; prefix != "" {
		if !strings.HasPrefix(value, prefix) {
			return false
		}
		value = value[len(prefix):]
	}

	if suffix := parts[len(parts)-1]; suffix != "" {
		if !strings.HasSuffix(value, suffix) {
			return false
		}
		value = value[:len(value)-len(suffix)]
	}

	for i := 1; i < len(parts)-1; i++ {
		segment := parts[i]
		if segment == "" {
			continue
		}
		idx := strings.Index(value, segment)
		if idx < 0 {
			return false
		}
		value = value[idx+len(segment):]
	}

	return true
}
