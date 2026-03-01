package orchestrator

import (
	"context"
	"fmt"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// Adversarial runs two specialized agents:
// implementer builds the change and reviewer tries to break or reject it.
type Adversarial struct{}

func (Adversarial) Name() string { return string(store.StrategyAdversarial) }

func (Adversarial) Plan(_ context.Context, task *store.Task) ([]AgentPlan, error) {
	return []AgentPlan{
		{
			Index:  0,
			Branch: fmt.Sprintf("orchestrate/%s/implementer", task.ID),
			Prompt: buildImplementerPrompt(task.Prompt),
		},
		{
			Index:  1,
			Branch: fmt.Sprintf("orchestrate/%s/reviewer", task.ID),
			Prompt: buildReviewerPrompt(task.Prompt),
		},
	}, nil
}

func (Adversarial) Evaluate(_ context.Context, _ *store.Task, results []AgentResult) (*Evaluation, error) {
	if len(results) < 2 {
		return &Evaluation{Success: false, Summary: "adversarial strategy requires 2 results"}, nil
	}

	implementer := results[0]
	reviewer := results[1]

	success := implementer.ExitCode == 0 && reviewer.ExitCode == 0
	return &Evaluation{
		Success: success,
		Summary: fmt.Sprintf(
			"implementer exit=%d reviewer exit=%d",
			implementer.ExitCode,
			reviewer.ExitCode,
		),
	}, nil
}

func buildImplementerPrompt(base string) string {
	return "ROLE: IMPLEMENTER\n" +
		"Goal: Implement the requested change in the repository.\n" +
		"Requirements:\n" +
		"- Prioritize correctness and minimal, focused edits.\n" +
		"- Run relevant checks/tests if available.\n" +
		"- In your final response, summarize files changed and validation performed.\n\n" +
		"REQUEST:\n" + base
}

func buildReviewerPrompt(base string) string {
	return "ROLE: REVIEWER (ADVERSARIAL)\n" +
		"Goal: Attempt to find correctness, security, and regression issues for the requested change.\n" +
		"Requirements:\n" +
		"- Focus on edge cases, failure modes, authz/authn risks, and test gaps.\n" +
		"- If you find blocking issues, report them clearly and exit non-zero.\n" +
		"- If no blocking issues are found, exit zero with a concise rationale.\n\n" +
		"REQUEST UNDER REVIEW:\n" + base
}
