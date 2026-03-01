package agent

import (
	"context"
	"fmt"
	"os"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

// Codex implements Agent using the Codex CLI.
type Codex struct {
	sb sandbox.Sandbox
}

// NewCodex creates a Codex agent backed by the given sandbox.
func NewCodex(sb sandbox.Sandbox) *Codex {
	return &Codex{sb: sb}
}

func (c *Codex) Run(ctx context.Context, ws *sandbox.Workspace, prompt string, opts RunOpts) (*Result, error) {
	cmd := []string{
		"codex",
		"exec",
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
	}

	if opts.Model != "" {
		cmd = append(cmd, "--model", opts.Model)
	}

	cmd = append(cmd, prompt)

	res, err := c.sb.Exec(ctx, ws, cmd)
	if err != nil {
		return nil, fmt.Errorf("codex exec: %w", err)
	}

	result := &Result{
		ExitCode: res.ExitCode,
		Output:   res.Stdout,
		Stderr:   res.Stderr,
	}

	if opts.LogPath != "" {
		_ = os.WriteFile(opts.LogPath, []byte(res.Stdout+"\n---STDERR---\n"+res.Stderr), 0o600)
	}

	return result, nil
}
