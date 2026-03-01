package orchestrator

import (
	"context"
	"testing"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// --- Implement ---

func TestImplementPlan(t *testing.T) {
	t.Parallel()
	s := Implement{}
	task := &store.Task{ID: "abc123", Prompt: "do stuff"}
	plans, err := s.Plan(context.Background(), task)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	if plans[0].Branch != "orchestrate/abc123" {
		t.Errorf("branch = %q, want orchestrate/abc123", plans[0].Branch)
	}
	if plans[0].Prompt != "do stuff" {
		t.Errorf("prompt = %q", plans[0].Prompt)
	}
	if plans[0].ReadOnly {
		t.Error("ReadOnly should be false")
	}
}

func TestImplementEvaluateNoResults(t *testing.T) {
	t.Parallel()
	s := Implement{}
	eval, err := s.Evaluate(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Success {
		t.Error("expected failure with no results")
	}
}

func TestImplementEvaluateSuccess(t *testing.T) {
	t.Parallel()
	s := Implement{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{Index: 0, ExitCode: 0, Output: "ok"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !eval.Success {
		t.Error("expected success for exit code 0")
	}
}

func TestImplementEvaluateFailure(t *testing.T) {
	t.Parallel()
	s := Implement{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{Index: 0, ExitCode: 1, Output: "error"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Success {
		t.Error("expected failure for exit code 1")
	}
	if eval.Summary != "agent exited with code 1" {
		t.Errorf("summary = %q", eval.Summary)
	}
}

func TestImplementName(t *testing.T) {
	t.Parallel()
	s := Implement{}
	if s.Name() != "IMPLEMENT" {
		t.Errorf("name = %q", s.Name())
	}
}

// --- Investigate ---

func TestInvestigatePlanReadOnly(t *testing.T) {
	t.Parallel()
	s := Investigate{}
	task := &store.Task{ID: "inv1", Prompt: "analyze"}
	plans, err := s.Plan(context.Background(), task)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1", len(plans))
	}
	if !plans[0].ReadOnly {
		t.Error("Investigate should set ReadOnly = true")
	}
}

func TestInvestigateEvaluatePassesOutput(t *testing.T) {
	t.Parallel()
	s := Investigate{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{Index: 0, ExitCode: 0, Output: "analysis result"},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !eval.Success {
		t.Error("expected success")
	}
	if eval.Summary != "analysis result" {
		t.Errorf("summary = %q, want agent output", eval.Summary)
	}
}

func TestInvestigateEvaluateNoResults(t *testing.T) {
	t.Parallel()
	s := Investigate{}
	eval, err := s.Evaluate(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Success {
		t.Error("expected failure with no results")
	}
}

func TestInvestigateName(t *testing.T) {
	t.Parallel()
	s := Investigate{}
	if s.Name() != "INVESTIGATE" {
		t.Errorf("name = %q", s.Name())
	}
}

// --- Compete ---

func TestCompetePlanDefaultsToTwo(t *testing.T) {
	t.Parallel()
	s := Compete{}
	task := &store.Task{ID: "c1", Prompt: "compete", AgentCount: 0}
	plans, err := s.Plan(context.Background(), task)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plans) != 2 {
		t.Fatalf("got %d plans, want 2 (default)", len(plans))
	}
}

func TestCompetePlanNAgents(t *testing.T) {
	t.Parallel()
	s := Compete{}
	task := &store.Task{ID: "c2", Prompt: "compete", AgentCount: 4}
	plans, err := s.Plan(context.Background(), task)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plans) != 4 {
		t.Fatalf("got %d plans, want 4", len(plans))
	}
	for i, p := range plans {
		if p.Index != i {
			t.Errorf("plans[%d].Index = %d", i, p.Index)
		}
	}
	if plans[2].Branch != "orchestrate/c2/2" {
		t.Errorf("branch = %q", plans[2].Branch)
	}
}

func TestCompeteEvaluateAnySuccess(t *testing.T) {
	t.Parallel()
	s := Compete{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{ExitCode: 1},
		{ExitCode: 0},
		{ExitCode: 1},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !eval.Success {
		t.Error("expected success when at least one agent succeeds")
	}
}

func TestCompeteEvaluateAllFail(t *testing.T) {
	t.Parallel()
	s := Compete{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{ExitCode: 1},
		{ExitCode: 2},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Success {
		t.Error("expected failure when all agents fail")
	}
}

func TestCompeteName(t *testing.T) {
	t.Parallel()
	s := Compete{}
	if s.Name() != "COMPETE" {
		t.Errorf("name = %q", s.Name())
	}
}

// --- Batch ---

func TestBatchPlanDefaultsToOne(t *testing.T) {
	t.Parallel()
	s := Batch{}
	task := &store.Task{ID: "b1", Prompt: "batch", AgentCount: 0}
	plans, err := s.Plan(context.Background(), task)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plans) != 1 {
		t.Fatalf("got %d plans, want 1 (default)", len(plans))
	}
}

func TestBatchPlanNAgents(t *testing.T) {
	t.Parallel()
	s := Batch{}
	task := &store.Task{ID: "b2", Prompt: "batch", AgentCount: 3}
	plans, err := s.Plan(context.Background(), task)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("got %d plans, want 3", len(plans))
	}
}

func TestBatchEvaluateAllSucceed(t *testing.T) {
	t.Parallel()
	s := Batch{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{ExitCode: 0},
		{ExitCode: 0},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !eval.Success {
		t.Error("expected success when all agents succeed")
	}
}

func TestBatchEvaluatePartialFailure(t *testing.T) {
	t.Parallel()
	s := Batch{}
	eval, err := s.Evaluate(context.Background(), nil, []AgentResult{
		{ExitCode: 0},
		{ExitCode: 1},
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if eval.Success {
		t.Error("expected failure when not all agents succeed")
	}
}

func TestBatchName(t *testing.T) {
	t.Parallel()
	s := Batch{}
	if s.Name() != "BATCH" {
		t.Errorf("name = %q", s.Name())
	}
}
