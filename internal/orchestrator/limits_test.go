package orchestrator

import (
	"context"
	"testing"

	"github.com/SCKelemen/orchestrate/internal/store"
)

func TestCompetePlanRejectsTooManyAgents(t *testing.T) {
	t.Parallel()

	_, err := (Compete{}).Plan(context.Background(), &store.Task{
		ID:         "t1",
		Prompt:     "test",
		AgentCount: maxPlannedAgents + 1,
	})
	if err == nil {
		t.Fatal("expected error for excessive agent_count")
	}
}

func TestBatchPlanRejectsTooManyAgents(t *testing.T) {
	t.Parallel()

	_, err := (Batch{}).Plan(context.Background(), &store.Task{
		ID:         "t1",
		Prompt:     "test",
		AgentCount: maxPlannedAgents + 1,
	})
	if err == nil {
		t.Fatal("expected error for excessive agent_count")
	}
}
