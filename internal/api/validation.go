package api

import (
	"fmt"
	"strings"

	"github.com/SCKelemen/orchestrate/internal/agent"
	"github.com/SCKelemen/orchestrate/internal/store"
)

const (
	// Keep agent fan-out bounded to avoid untrusted requests causing memory/goroutine exhaustion.
	maxAgentCount     = 32
	maxPromptSize     = 64 * 1024
	defaultAgentImage = "orchestrate-agent:latest"
)

func normalizeStrategy(raw string) (store.Strategy, error) {
	if raw == "" {
		return store.StrategyImplement, nil
	}

	s := store.Strategy(raw)
	switch s {
	case store.StrategyImplement,
		store.StrategyInvestigate,
		store.StrategyCompete,
		store.StrategyBatch,
		store.StrategyAdversarial,
		store.StrategyCodeAndTest:
		return s, nil
	default:
		return "", fmt.Errorf("unsupported strategy: %s", raw)
	}
}

func normalizeAgentCount(n int) (int, error) {
	if n <= 0 {
		return 1, nil
	}
	if n > maxAgentCount {
		return 0, fmt.Errorf("agentCount must be between 1 and %d", maxAgentCount)
	}
	return n, nil
}

func validatePromptSize(prompt string) error {
	if len(prompt) > maxPromptSize {
		return fmt.Errorf("prompt exceeds %d bytes", maxPromptSize)
	}
	return nil
}

func normalizeAgentBackend(raw string) (string, error) {
	return agent.NormalizeBackend(raw)
}

func normalizeImage(raw string, allowed map[string]struct{}, allowAny bool) (string, error) {
	image := strings.TrimSpace(raw)
	if image == "" {
		image = defaultAgentImage
	}
	if allowAny {
		return image, nil
	}
	if _, ok := allowed[image]; !ok {
		return "", fmt.Errorf("image is not allowed: %s", image)
	}
	return image, nil
}
