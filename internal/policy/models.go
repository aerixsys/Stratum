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

var (
	getExecutablePath = os.Executable
	getWorkingDir     = os.Getwd
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

// LoadDefaultModelPolicy resolves and loads the default model policy file.
func LoadDefaultModelPolicy(configuredPath string) (*ModelPolicy, error) {
	path, err := ResolveDefaultPolicyPath(configuredPath)
	if err != nil {
		return nil, err
	}
	return LoadModelPolicy(path)
}

// ResolveDefaultPolicyPath resolves the policy path in deterministic order:
// 1) explicit path, 2) executable-relative config/model-policy.yaml, 3) cwd-local config/model-policy.yaml.
func ResolveDefaultPolicyPath(configuredPath string) (string, error) {
	candidates := make([]string, 0, 3)
	seen := make(map[string]struct{})
	addCandidate := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		if _, exists := seen[p]; exists {
			return
		}
		seen[p] = struct{}{}
		candidates = append(candidates, p)
	}

	addCandidate(configuredPath)

	execPath, execErr := getExecutablePath()
	if execErr == nil {
		addCandidate(filepath.Join(filepath.Dir(execPath), defaultPolicyRelativePath))
	}

	wd, wdErr := getWorkingDir()
	if wdErr == nil {
		addCandidate(filepath.Join(wd, defaultPolicyRelativePath))
	}

	if len(candidates) == 0 {
		switch {
		case execErr != nil && wdErr != nil:
			return "", fmt.Errorf("resolve default policy path: executable error: %v; cwd error: %v", execErr, wdErr)
		case execErr != nil:
			return "", fmt.Errorf("resolve default policy path: executable error: %v", execErr)
		case wdErr != nil:
			return "", fmt.Errorf("resolve default policy path: cwd error: %v", wdErr)
		default:
			return "", fmt.Errorf("resolve default policy path: no candidates available")
		}
	}

	tried := make([]string, 0, len(candidates))
	for _, candidate := range candidates {
		tried = append(tried, candidate)
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
	}

	return "", fmt.Errorf("model policy file %s not found; tried: %s", defaultPolicyRelativePath, strings.Join(tried, ", "))
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
