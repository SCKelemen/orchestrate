package orchestrator

import (
	"context"
	"fmt"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// Batch fans out N tasks to N agents, each with the same prompt.
type Batch struct{}

func (Batch) Name() string { return "BATCH" }

func (Batch) Plan(_ context.Context, task *store.Task) ([]AgentPlan, error) {
	n := task.AgentCount
	if n < 1 {
		n = 1
	}
	if n > maxPlannedAgents {
		return nil, fmt.Errorf("agent_count %d exceeds max %d", n, maxPlannedAgents)
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

func (Batch) Evaluate(_ context.Context, _ *store.Task, results []AgentResult) (*Evaluation, error) {
	successes := 0
	for _, r := range results {
		if r.ExitCode == 0 {
			successes++
		}
	}
	return &Evaluation{
		Success: successes == len(results),
		Summary: fmt.Sprintf("%d/%d agents succeeded", successes, len(results)),
	}, nil
}
