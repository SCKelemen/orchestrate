package orchestrator

import (
	"context"
	"fmt"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// Investigate is a read-only strategy for analysis tasks.
type Investigate struct{}

func (Investigate) Name() string { return "INVESTIGATE" }

func (Investigate) Plan(_ context.Context, task *store.Task) ([]AgentPlan, error) {
	branch := fmt.Sprintf("orchestrate/%s", task.ID)
	return []AgentPlan{
		{
			Index:    0,
			Branch:   branch,
			Prompt:   task.Prompt,
			ReadOnly: true,
		},
	}, nil
}

func (Investigate) Evaluate(_ context.Context, _ *store.Task, results []AgentResult) (*Evaluation, error) {
	if len(results) == 0 {
		return &Evaluation{Success: false, Summary: "no results"}, nil
	}
	r := results[0]
	return &Evaluation{
		Success: r.ExitCode == 0,
		Summary: r.Output,
	}, nil
}
