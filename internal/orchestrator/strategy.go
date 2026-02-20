package orchestrator

import (
	"context"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// AgentPlan describes what a single agent should do.
type AgentPlan struct {
	Index    int
	Branch   string
	Prompt   string
	ReadOnly bool
}

// AgentResult holds the outcome of a single agent execution.
type AgentResult struct {
	Index    int
	RunID    string
	ExitCode int
	Output   string
}

// Evaluation is the strategy's assessment of the results.
type Evaluation struct {
	Success bool
	Summary string
}

// Strategy plans and evaluates agent executions for a task.
type Strategy interface {
	Name() string
	Plan(ctx context.Context, task *store.Task) ([]AgentPlan, error)
	Evaluate(ctx context.Context, task *store.Task, results []AgentResult) (*Evaluation, error)
}
