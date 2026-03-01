package orchestrator

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SCKelemen/orchestrate/internal/agent"
	"github.com/SCKelemen/orchestrate/internal/sandbox"
	"github.com/SCKelemen/orchestrate/internal/store"
)

type testSandbox struct {
	createCount int
	lastCreate  sandbox.CreateOpts
}

func (s *testSandbox) Create(ctx context.Context, opts sandbox.CreateOpts) (*sandbox.Workspace, error) {
	s.createCount++
	s.lastCreate = opts
	return &sandbox.Workspace{
		ID:      fmt.Sprintf("ws-test-%d", s.createCount),
		RepoURL: opts.RepoURL,
		Branch:  opts.Branch,
	}, nil
}

func (s *testSandbox) Exec(ctx context.Context, ws *sandbox.Workspace, cmd []string) (*sandbox.ExecResult, error) {
	return &sandbox.ExecResult{}, nil
}

func (s *testSandbox) Destroy(ctx context.Context, ws *sandbox.Workspace) error {
	return nil
}

type testAgent struct {
	called bool
	prompt string
	calls  []agentCall
	result *agent.Result
}

type agentCall struct {
	ws     *sandbox.Workspace
	prompt string
}

func (a *testAgent) Run(ctx context.Context, ws *sandbox.Workspace, prompt string, opts agent.RunOpts) (*agent.Result, error) {
	a.called = true
	a.prompt = prompt
	a.calls = append(a.calls, agentCall{ws: ws, prompt: prompt})
	if a.result == nil {
		return &agent.Result{}, nil
	}
	return a.result, nil
}

func TestExecuteAgentNormalizesBackendAlias(t *testing.T) {
	ctx := context.Background()
	s := openTestStore(t)
	t.Setenv("OPENAI_API_KEY", "test-openai-key")

	task, err := s.CreateTask(ctx, "t-openai", store.CreateTaskParams{
		OwnerUserID: "u1",
		Agent:       "openai",
		Prompt:      "do work",
		RepoURL:     "https://example.com/repo.git",
		Strategy:    store.StrategyImplement,
		AgentCount:  1,
		Image:       "orchestrate-agent:latest",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	codexAgent := &testAgent{result: &agent.Result{ExitCode: 0, Output: "ok"}}
	sb := &testSandbox{}
	orch := New(
		s,
		sb,
		map[string]agent.Agent{
			agent.BackendCodex: codexAgent,
		},
		agent.BackendClaude,
		t.TempDir(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	res, err := orch.executeAgent(ctx, task, AgentPlan{
		Index:  0,
		Branch: "feature/test",
		Prompt: "hello from test",
	})
	if err != nil {
		t.Fatalf("execute agent: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d want=0", res.ExitCode)
	}
	if !codexAgent.called {
		t.Fatal("expected codex backend to be called")
	}
	if codexAgent.prompt != "hello from test" {
		t.Fatalf("prompt=%q want=%q", codexAgent.prompt, "hello from test")
	}
	if got := sb.lastCreate.EnvVars["OPENAI_API_KEY"]; got != "test-openai-key" {
		t.Fatalf("OPENAI_API_KEY=%q want=%q", got, "test-openai-key")
	}

	runs, err := s.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs=%d want=1", len(runs))
	}
	if runs[0].State != store.RunSucceeded {
		t.Fatalf("run state=%s want=%s", runs[0].State, store.RunSucceeded)
	}
}

func TestExecuteAgentFailsForUnsupportedBackend(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)

	task, err := s.CreateTask(ctx, "t-unknown", store.CreateTaskParams{
		OwnerUserID: "u1",
		Agent:       "unknown",
		Prompt:      "do work",
		RepoURL:     "https://example.com/repo.git",
		Strategy:    store.StrategyImplement,
		AgentCount:  1,
		Image:       "orchestrate-agent:latest",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sb := &testSandbox{}
	orch := New(
		s,
		sb,
		map[string]agent.Agent{
			agent.BackendClaude: &testAgent{result: &agent.Result{ExitCode: 0, Output: "ok"}},
		},
		agent.BackendClaude,
		t.TempDir(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	_, err = orch.executeAgent(ctx, task, AgentPlan{
		Index:  0,
		Branch: "feature/test",
		Prompt: "hello from test",
	})
	if err == nil {
		t.Fatal("expected error for unsupported backend")
	}
	if !strings.Contains(err.Error(), "unsupported agent backend") {
		t.Fatalf("unexpected error: %v", err)
	}

	runs, err := s.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs=%d want=1", len(runs))
	}
	if runs[0].State != store.RunFailed {
		t.Fatalf("run state=%s want=%s", runs[0].State, store.RunFailed)
	}
	if sb.createCount != 0 {
		t.Fatalf("createCount=%d want=0", sb.createCount)
	}
}

func TestExecuteAdversarialUsesSingleWorkspaceHandoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)

	task, err := s.CreateTask(ctx, "t-adv", store.CreateTaskParams{
		OwnerUserID: "u1",
		Agent:       "claude",
		Prompt:      "implement endpoint and review it",
		RepoURL:     "https://example.com/repo.git",
		Strategy:    store.StrategyAdversarial,
		AgentCount:  2,
		Image:       "orchestrate-agent:latest",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sb := &testSandbox{}
	ag := &testAgent{result: &agent.Result{ExitCode: 0, Output: "ok"}}
	orch := New(
		s,
		sb,
		map[string]agent.Agent{
			agent.BackendClaude: ag,
		},
		agent.BackendClaude,
		t.TempDir(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	if err := orch.Execute(ctx, task); err != nil {
		t.Fatalf("execute task: %v", err)
	}

	if sb.createCount != 1 {
		t.Fatalf("workspace createCount=%d want=1", sb.createCount)
	}
	if len(ag.calls) != 2 {
		t.Fatalf("agent call count=%d want=2", len(ag.calls))
	}
	if ag.calls[0].ws != ag.calls[1].ws {
		t.Fatal("expected both adversarial runs to share the same workspace")
	}

	runs, err := s.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs=%d want=2", len(runs))
	}
	if runs[0].Branch != runs[1].Branch {
		t.Fatalf("expected shared branch; got %q and %q", runs[0].Branch, runs[1].Branch)
	}

	updatedTask, err := s.GetTask(ctx, task.ID)
	if err != nil || updatedTask == nil {
		t.Fatalf("get task err=%v task=%v", err, updatedTask)
	}
	if updatedTask.State != store.TaskSucceeded {
		t.Fatalf("task state=%s want=%s", updatedTask.State, store.TaskSucceeded)
	}
}

func TestExecuteCodeAndTestUsesSingleWorkspaceHandoff(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	s := openTestStore(t)

	task, err := s.CreateTask(ctx, "t-code-test", store.CreateTaskParams{
		OwnerUserID: "u1",
		Agent:       "claude",
		Prompt:      "implement with tests",
		RepoURL:     "https://example.com/repo.git",
		Strategy:    store.StrategyCodeAndTest,
		AgentCount:  2,
		Image:       "orchestrate-agent:latest",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}

	sb := &testSandbox{}
	ag := &testAgent{result: &agent.Result{ExitCode: 0, Output: "ok"}}
	orch := New(
		s,
		sb,
		map[string]agent.Agent{
			agent.BackendClaude: ag,
		},
		agent.BackendClaude,
		t.TempDir(),
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	if err := orch.Execute(ctx, task); err != nil {
		t.Fatalf("execute task: %v", err)
	}

	if sb.createCount != 1 {
		t.Fatalf("workspace createCount=%d want=1", sb.createCount)
	}
	if len(ag.calls) != 2 {
		t.Fatalf("agent call count=%d want=2", len(ag.calls))
	}
	if ag.calls[0].ws != ag.calls[1].ws {
		t.Fatal("expected both code/test runs to share the same workspace")
	}

	runs, err := s.ListRuns(ctx, task.ID)
	if err != nil {
		t.Fatalf("list runs: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("runs=%d want=2", len(runs))
	}
	if runs[0].Branch != runs[1].Branch {
		t.Fatalf("expected shared branch; got %q and %q", runs[0].Branch, runs[1].Branch)
	}
}

func openTestStore(t *testing.T) *store.Store {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "orchestrator-test.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}
