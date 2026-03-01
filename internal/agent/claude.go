package agent

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

// Claude implements Agent using the Claude Code CLI.
type Claude struct {
	sb sandbox.Sandbox
}

// NewClaude creates a Claude agent backed by the given sandbox.
func NewClaude(sb sandbox.Sandbox) *Claude {
	return &Claude{sb: sb}
}

func (c *Claude) Run(ctx context.Context, ws *sandbox.Workspace, prompt string, opts RunOpts) (*Result, error) {
	cmd := []string{
		"claude",
		"-p", prompt,
		"--output-format", "json",
		"--dangerously-skip-permissions",
	}

	if opts.Model != "" {
		cmd = append(cmd, "--model", opts.Model)
	}
	if opts.MaxTurns > 0 {
		cmd = append(cmd, "--max-turns", fmt.Sprintf("%d", opts.MaxTurns))
	}
	if opts.MaxBudgetUSD > 0 {
		cmd = append(cmd, "--max-budget-usd", fmt.Sprintf("%.2f", opts.MaxBudgetUSD))
	}
	if len(opts.AllowedTools) > 0 {
		cmd = append(cmd, "--allowedTools", strings.Join(opts.AllowedTools, ","))
	}

	res, err := c.sb.Exec(ctx, ws, cmd)
	if err != nil {
		return nil, fmt.Errorf("claude exec: %w", err)
	}

	result := &Result{
		ExitCode: res.ExitCode,
		Output:   res.Stdout,
		Stderr:   res.Stderr,
	}

	// Write log file if requested
	if opts.LogPath != "" {
		_ = os.WriteFile(opts.LogPath, []byte(res.Stdout+"\n---STDERR---\n"+res.Stderr), 0o600)
	}

	return result, nil
}
