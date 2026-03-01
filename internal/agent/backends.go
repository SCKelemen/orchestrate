package agent

import (
	"fmt"
	"strings"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

const (
	BackendClaude = "claude"
	BackendCodex  = "codex"
)

// NormalizeBackend canonicalizes backend aliases.
func NormalizeBackend(raw string) (string, error) {
	v := strings.TrimSpace(strings.ToLower(raw))
	if v == "" {
		return BackendClaude, nil
	}
	switch v {
	case BackendClaude, "anthropic":
		return BackendClaude, nil
	case BackendCodex, "openai":
		return BackendCodex, nil
	default:
		return "", fmt.Errorf("unsupported agent backend: %s", raw)
	}
}

// NewBackends returns the built-in backend registry keyed by canonical name.
func NewBackends(sb sandbox.Sandbox) map[string]Agent {
	return map[string]Agent{
		BackendClaude: NewClaude(sb),
		BackendCodex:  NewCodex(sb),
	}
}
