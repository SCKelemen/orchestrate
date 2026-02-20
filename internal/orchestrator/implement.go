package orchestrator

import (
	"context"
	"fmt"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// Implement is the simplest strategy: one agent, one branch.
type Implement struct{}

func (Implement) Name() string { return "IMPLEMENT" }

func (Implement) Plan(_ context.Context, task *store.Task) ([]AgentPlan, error) {
	branch := fmt.Sprintf("orchestrate/%s", task.ID)
	return []AgentPlan{
		{
			Index:  0,
			Branch: branch,
			Prompt: task.Prompt,
		},
	}, nil
}

func (Implement) Evaluate(_ context.Context, _ *store.Task, results []AgentResult) (*Evaluation, error) {
	if len(results) == 0 {
		return &Evaluation{Success: false, Summary: "no results"}, nil
	}
	r := results[0]
	if r.ExitCode == 0 {
		return &Evaluation{Success: true, Summary: "agent completed successfully"}, nil
	}
	return &Evaluation{
		Success: false,
		Summary: fmt.Sprintf("agent exited with code %d", r.ExitCode),
	}, nil
}
