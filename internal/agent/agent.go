package agent

import (
	"context"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

// Result holds the output of an agent execution.
type Result struct {
	ExitCode int
	Output   string
	Stderr   string
}

// Agent runs a prompt in a sandbox workspace.
type Agent interface {
	Run(ctx context.Context, ws *sandbox.Workspace, prompt string, opts RunOpts) (*Result, error)
}

// RunOpts are options for an agent run.
type RunOpts struct {
	Model        string
	MaxTurns     int
	MaxBudgetUSD float64
	AllowedTools []string
	OutputFormat string
	LogPath      string
}
