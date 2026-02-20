package orchestrator

import (
	"context"
	"fmt"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// Compete runs N agents in parallel, each on their own branch.
type Compete struct{}

func (Compete) Name() string { return "COMPETE" }

func (Compete) Plan(_ context.Context, task *store.Task) ([]AgentPlan, error) {
	n := task.AgentCount
	if n < 2 {
		n = 2
	}
	plans := make([]AgentPlan, n)
	for i := range n {
		plans[i] = AgentPlan{
			Index:  i,
			Branch: fmt.Sprintf("orchestrate/%s/%d", task.ID, i),
			Prompt: task.Prompt,
		}
	}
	return plans, nil
}

func (Compete) Evaluate(_ context.Context, _ *store.Task, results []AgentResult) (*Evaluation, error) {
	successes := 0
	for _, r := range results {
		if r.ExitCode == 0 {
			successes++
		}
	}
	return &Evaluation{
		Success: successes > 0,
		Summary: fmt.Sprintf("%d/%d agents succeeded", successes, len(results)),
	}, nil
}
