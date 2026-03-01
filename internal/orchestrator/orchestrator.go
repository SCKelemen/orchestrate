package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SCKelemen/orchestrate/internal/agent"
	"github.com/SCKelemen/orchestrate/internal/sandbox"
	"github.com/SCKelemen/orchestrate/internal/store"
	"golang.org/x/sync/errgroup"
)

// Orchestrator coordinates agent execution for tasks.
type Orchestrator struct {
	store        *store.Store
	sandbox      sandbox.Sandbox
	agents       map[string]agent.Agent
	defaultAgent string
	strategies   map[store.Strategy]Strategy
	dataDir      string
	logger       *slog.Logger
}

// New creates a new Orchestrator.
func New(s *store.Store, sb sandbox.Sandbox, agents map[string]agent.Agent, defaultAgent string, dataDir string, logger *slog.Logger) *Orchestrator {
	if defaultAgent == "" {
		defaultAgent = agent.BackendClaude
	}
	o := &Orchestrator{
		store:        s,
		sandbox:      sb,
		agents:       agents,
		defaultAgent: defaultAgent,
		dataDir:      dataDir,
		logger:       logger,
		strategies: map[store.Strategy]Strategy{
			store.StrategyImplement:   Implement{},
			store.StrategyInvestigate: Investigate{},
			store.StrategyCompete:     Compete{},
			store.StrategyBatch:       Batch{},
		},
	}
	return o
}

// Execute runs a single task through its strategy lifecycle.
func (o *Orchestrator) Execute(ctx context.Context, task *store.Task) error {
	strategy, ok := o.strategies[task.Strategy]
	if !ok {
		return fmt.Errorf("unknown strategy: %s", task.Strategy)
	}

	o.logger.Info("executing task", "task", task.ID, "strategy", strategy.Name())

	// Plan
	plans, err := strategy.Plan(ctx, task)
	if err != nil {
		o.failTask(ctx, task.ID)
		return fmt.Errorf("plan: %w", err)
	}

	// Execute agents in parallel
	results := make([]AgentResult, len(plans))
	g, gctx := errgroup.WithContext(ctx)

	for i, plan := range plans {
		g.Go(func() error {
			result, err := o.executeAgent(gctx, task, plan)
			if err != nil {
				o.logger.Error("agent failed", "task", task.ID, "agent", i, "error", err)
				results[i] = AgentResult{Index: i, ExitCode: 1, Output: err.Error()}
				return nil // don't fail the group
			}
			results[i] = *result
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		o.failTask(ctx, task.ID)
		return fmt.Errorf("execute agents: %w", err)
	}

	// Evaluate
	eval, err := strategy.Evaluate(ctx, task, results)
	if err != nil {
		o.failTask(ctx, task.ID)
		return fmt.Errorf("evaluate: %w", err)
	}

	o.logger.Info("task evaluation", "task", task.ID, "success", eval.Success, "summary", eval.Summary)

	if eval.Success {
		return o.store.UpdateTaskState(ctx, task.ID, store.TaskSucceeded)
	}
	return o.store.UpdateTaskState(ctx, task.ID, store.TaskFailed)
}

func (o *Orchestrator) executeAgent(ctx context.Context, task *store.Task, plan AgentPlan) (*AgentResult, error) {
	runID := newID()
	logDir := filepath.Join(o.dataDir, "logs")
	logPath := filepath.Join(logDir, runID+".log")

	// Create run record
	_, err := o.store.CreateRun(ctx, runID, store.CreateRunParams{
		TaskID:     task.ID,
		AgentIndex: plan.Index,
		Branch:     plan.Branch,
		LogPath:    logPath,
	})
	if err != nil {
		return nil, fmt.Errorf("create run: %w", err)
	}

	// Select backend for this task.
	backendInput := task.Agent
	if backendInput == "" {
		backendInput = o.defaultAgent
	}
	backend, err := agent.NormalizeBackend(backendInput)
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, err
	}
	ag, ok := o.agents[backend]
	if !ok {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), "unsupported agent backend: "+backend)
		return nil, fmt.Errorf("unsupported agent backend: %s", backend)
	}

	// Create sandbox workspace
	ws, err := o.sandbox.Create(ctx, sandbox.CreateOpts{
		Image:   task.Image,
		RepoURL: task.RepoURL,
		BaseRef: task.BaseRef,
		Branch:  plan.Branch,
		EnvVars: envVarsForBackend(backend),
	})
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	defer o.sandbox.Destroy(ctx, ws)

	// Mark run as running
	o.store.UpdateRunState(ctx, runID, store.RunRunning, nil, "")

	// Execute agent
	result, err := ag.Run(ctx, ws, plan.Prompt, agent.RunOpts{
		OutputFormat: "json",
		LogPath:      logPath,
	})
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, fmt.Errorf("agent run: %w", err)
	}

	// Update run state
	state := store.RunSucceeded
	if result.ExitCode != 0 {
		state = store.RunFailed
	}
	o.store.UpdateRunState(ctx, runID, state, &result.ExitCode, result.Output)

	return &AgentResult{
		Index:    plan.Index,
		RunID:    runID,
		ExitCode: result.ExitCode,
		Output:   result.Output,
	}, nil
}

func (o *Orchestrator) failTask(ctx context.Context, taskID string) {
	if err := o.store.UpdateTaskState(ctx, taskID, store.TaskFailed); err != nil {
		o.logger.Error("failed to mark task as failed", "task", taskID, "error", err)
	}
}

func intPtr(v int) *int { return &v }

func envVarsForBackend(backend string) map[string]string {
	env := map[string]string{}
	switch backend {
	case agent.BackendClaude:
		copyEnvIfSet(env, "ANTHROPIC_API_KEY")
		copyEnvIfSet(env, "ANTHROPIC_BASE_URL")
	case agent.BackendCodex:
		copyEnvIfSet(env, "OPENAI_API_KEY")
		copyEnvIfSet(env, "OPENAI_BASE_URL")
	}
	return env
}

func copyEnvIfSet(dst map[string]string, key string) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return
	}
	dst[key] = v
}

func newID() string {
	ts := time.Now().UnixMilli()
	b := make([]byte, 16)
	b[0] = byte(ts >> 40)
	b[1] = byte(ts >> 32)
	b[2] = byte(ts >> 24)
	b[3] = byte(ts >> 16)
	b[4] = byte(ts >> 8)
	b[5] = byte(ts)
	if _, err := rand.Read(b[6:]); err != nil {
		// Fall back to timestamp-derived bytes if CSPRNG is unavailable.
		ns := time.Now().UnixNano()
		for i := 6; i < len(b); i++ {
			shift := uint((i - 6) * 8)
			b[i] = byte(ns >> shift)
		}
	}
	return hex.EncodeToString(b)
}
