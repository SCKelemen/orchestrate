package orchestrator

import (
	"context"
	"fmt"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// CodeAndTest splits work into dedicated coding and testing agents.
type CodeAndTest struct{}

func (CodeAndTest) Name() string { return string(store.StrategyCodeAndTest) }

func (CodeAndTest) Plan(_ context.Context, task *store.Task) ([]AgentPlan, error) {
	return []AgentPlan{
		{
			Index:  0,
			Branch: fmt.Sprintf("orchestrate/%s/code", task.ID),
			Prompt: buildCodeWriterPrompt(task.Prompt),
		},
		{
			Index:  1,
			Branch: fmt.Sprintf("orchestrate/%s/tests", task.ID),
			Prompt: buildTestWriterPrompt(task.Prompt),
		},
	}, nil
}

func (CodeAndTest) Evaluate(_ context.Context, _ *store.Task, results []AgentResult) (*Evaluation, error) {
	if len(results) < 2 {
		return &Evaluation{Success: false, Summary: "code_and_test strategy requires 2 results"}, nil
	}

	codeWriter := results[0]
	testWriter := results[1]
	success := codeWriter.ExitCode == 0 && testWriter.ExitCode == 0

	return &Evaluation{
		Success: success,
		Summary: fmt.Sprintf("code-writer exit=%d test-writer exit=%d", codeWriter.ExitCode, testWriter.ExitCode),
	}, nil
}

func buildCodeWriterPrompt(base string) string {
	return "ROLE: CODE WRITER\n" +
		"Goal: Implement the requested functionality with production-quality code.\n" +
		"Requirements:\n" +
		"- Prioritize clarity, maintainability, and correctness.\n" +
		"- Keep edits scoped to the task.\n" +
		"- In your final response, summarize code changes and runtime/test checks.\n\n" +
		"REQUEST:\n" + base
}

func buildTestWriterPrompt(base string) string {
	return "ROLE: TEST WRITER\n" +
		"Goal: Add or improve tests for the requested functionality.\n" +
		"Requirements:\n" +
		"- Focus on behavior, edge cases, and regression protection.\n" +
		"- Prefer deterministic, fast tests.\n" +
		"- If critical behavior cannot be tested, clearly report the gap.\n\n" +
		"REQUEST TO TEST:\n" + base
}
