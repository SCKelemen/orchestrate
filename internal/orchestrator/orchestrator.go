package orchestrator

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path"
	"path/filepath"
	"sort"
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

type sequentialHandoffStrategy interface {
	SequentialHandoff() bool
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
			store.StrategyAdversarial: Adversarial{},
			store.StrategyCodeAndTest: CodeAndTest{},
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

	var results []AgentResult
	if usesSequentialHandoff(strategy) {
		results = o.executeAgentsSequential(ctx, task, plans)
	} else {
		results = o.executeAgentsParallel(ctx, task, plans)
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

func usesSequentialHandoff(strategy Strategy) bool {
	s, ok := strategy.(sequentialHandoffStrategy)
	return ok && s.SequentialHandoff()
}

func (o *Orchestrator) executeAgentsParallel(ctx context.Context, task *store.Task, plans []AgentPlan) []AgentResult {
	results := make([]AgentResult, len(plans))
	g, gctx := errgroup.WithContext(ctx)

	for i, plan := range plans {
		i := i
		plan := plan
		g.Go(func() error {
			result, err := o.executeAgent(gctx, task, plan)
			if err != nil {
				o.logger.Error("agent failed", "task", task.ID, "agent", i, "error", err)
				results[i] = AgentResult{Index: plan.Index, ExitCode: 1, Output: err.Error()}
				return nil // don't fail the group
			}
			results[i] = *result
			return nil
		})
	}

	_ = g.Wait()
	return results
}

func (o *Orchestrator) executeAgentsSequential(ctx context.Context, task *store.Task, plans []AgentPlan) []AgentResult {
	results := make([]AgentResult, len(plans))

	backend, ag, err := o.resolveTaskAgent(task)
	if err != nil {
		for i, plan := range plans {
			results[i] = o.recordPlanFailure(ctx, task, plan, err.Error())
		}
		return results
	}

	sharedBranch := sharedHandoffBranch(task.ID, plans)
	createOpts, err := o.sandboxCreateOpts(task, sharedBranch, backend)
	if err != nil {
		for i, plan := range plans {
			results[i] = o.recordPlanFailure(ctx, task, plan, err.Error())
		}
		return results
	}
	ws, err := o.sandbox.Create(ctx, createOpts)
	if err != nil {
		msg := fmt.Sprintf("create workspace: %v", err)
		for i, plan := range plans {
			plan.Branch = sharedBranch
			results[i] = o.recordPlanFailure(ctx, task, plan, msg)
		}
		return results
	}
	defer o.sandbox.Destroy(ctx, ws)

	for i, plan := range plans {
		plan.Branch = sharedBranch

		runID, logPath, err := o.createRunRecord(ctx, task, plan)
		if err != nil {
			results[i] = AgentResult{Index: plan.Index, ExitCode: 1, Output: err.Error()}
			continue
		}

		result, err := o.runPlanInWorkspace(ctx, task, plan, runID, logPath, ws, ag)
		if err != nil {
			results[i] = AgentResult{Index: plan.Index, RunID: runID, ExitCode: 1, Output: err.Error()}
			continue
		}
		results[i] = *result
	}

	return results
}

func (o *Orchestrator) executeAgent(ctx context.Context, task *store.Task, plan AgentPlan) (*AgentResult, error) {
	runID, logPath, err := o.createRunRecord(ctx, task, plan)
	if err != nil {
		return nil, err
	}

	backend, ag, err := o.resolveTaskAgent(task)
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, err
	}

	createOpts, err := o.sandboxCreateOpts(task, plan.Branch, backend)
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, err
	}

	// Create sandbox workspace
	ws, err := o.sandbox.Create(ctx, createOpts)
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, fmt.Errorf("create workspace: %w", err)
	}
	defer o.sandbox.Destroy(ctx, ws)

	return o.runPlanInWorkspace(ctx, task, plan, runID, logPath, ws, ag)
}

func (o *Orchestrator) createRunRecord(ctx context.Context, task *store.Task, plan AgentPlan) (string, string, error) {
	runID := newID()
	logDir := filepath.Join(o.dataDir, "logs")
	logPath := filepath.Join(logDir, runID+".log")

	_, err := o.store.CreateRun(ctx, runID, store.CreateRunParams{
		TaskID:     task.ID,
		AgentIndex: plan.Index,
		Branch:     plan.Branch,
		LogPath:    logPath,
	})
	if err != nil {
		return "", "", fmt.Errorf("create run: %w", err)
	}

	return runID, logPath, nil
}

func (o *Orchestrator) resolveTaskAgent(task *store.Task) (string, agent.Agent, error) {
	backendInput := task.Agent
	if backendInput == "" {
		backendInput = o.defaultAgent
	}
	backend, err := agent.NormalizeBackend(backendInput)
	if err != nil {
		return "", nil, err
	}
	ag, ok := o.agents[backend]
	if !ok {
		return "", nil, fmt.Errorf("unsupported agent backend: %s", backend)
	}
	return backend, ag, nil
}

func (o *Orchestrator) runPlanInWorkspace(ctx context.Context, task *store.Task, plan AgentPlan, runID, logPath string, ws *sandbox.Workspace, ag agent.Agent) (*AgentResult, error) {
	o.store.UpdateRunState(ctx, runID, store.RunRunning, nil, "")

	result, err := ag.Run(ctx, ws, plan.Prompt, agent.RunOpts{
		OutputFormat: "json",
		LogPath:      logPath,
	})
	if err != nil {
		o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), err.Error())
		return nil, fmt.Errorf("agent run: %w", err)
	}
	if err := o.enforceManifestWritePolicy(ctx, ws, task); err != nil {
		exitCode := result.ExitCode
		if exitCode == 0 {
			exitCode = 1
		}
		o.store.UpdateRunState(ctx, runID, store.RunFailed, &exitCode, err.Error())
		return nil, err
	}

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

func (o *Orchestrator) sandboxCreateOpts(task *store.Task, branch, backend string) (sandbox.CreateOpts, error) {
	opts := sandbox.CreateOpts{
		Image:   task.Image,
		RepoURL: task.RepoURL,
		BaseRef: task.BaseRef,
		Branch:  branch,
		EnvVars: envVarsForBackend(backend),
	}

	manifest, err := store.ParsePermissionManifest(task.Manifest)
	if err != nil {
		return sandbox.CreateOpts{}, fmt.Errorf("invalid task manifest: %w", err)
	}
	if len(manifest.Sandbox.Filesystem) > 0 {
		paths := make([]string, 0, len(manifest.Sandbox.Filesystem))
		for _, fs := range manifest.Sandbox.Filesystem {
			paths = append(paths, fs.Path)
		}
		opts.VisibleRepoPaths = paths
	}

	mode := strings.ToLower(strings.TrimSpace(manifest.Sandbox.Network.Mode))
	switch mode {
	case "", store.ManifestNetworkModeDefault:
		// Keep sandbox default.
	case store.ManifestNetworkModeNone:
		opts.NetworkMode = sandbox.NetworkModeNone
	case store.ManifestNetworkModeAllowlist:
		opts.NetworkMode = sandbox.NetworkModeAllowlist
		opts.AllowedEgressDomains = append([]string(nil), manifest.Sandbox.Network.Allow...)
	default:
		return sandbox.CreateOpts{}, fmt.Errorf("invalid task manifest network mode: %s", manifest.Sandbox.Network.Mode)
	}

	return opts, nil
}

type manifestWritePolicy struct {
	enforced bool
	allowAll bool
	paths    []string
}

func (p manifestWritePolicy) allows(changedPath string) bool {
	if !p.enforced || p.allowAll {
		return true
	}
	for _, allowed := range p.paths {
		if changedPath == allowed || strings.HasPrefix(changedPath, allowed+"/") {
			return true
		}
	}
	return false
}

func writePolicyFromManifest(raw string) (manifestWritePolicy, error) {
	manifest, err := store.ParsePermissionManifest(raw)
	if err != nil {
		return manifestWritePolicy{}, err
	}

	if len(manifest.Sandbox.Filesystem) == 0 {
		return manifestWritePolicy{}, nil
	}

	allow := map[string]struct{}{}
	for _, fs := range manifest.Sandbox.Filesystem {
		p, err := normalizePolicyPath(fs.Path)
		if err != nil {
			return manifestWritePolicy{}, err
		}
		if !filesystemAccessAllowsWrite(fs.Access) {
			continue
		}
		if p == "." {
			return manifestWritePolicy{
				enforced: true,
				allowAll: true,
			}, nil
		}
		allow[p] = struct{}{}
	}

	paths := make([]string, 0, len(allow))
	for p := range allow {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	return manifestWritePolicy{
		enforced: true,
		paths:    paths,
	}, nil
}

func normalizePolicyPath(raw string) (string, error) {
	p := strings.TrimSpace(raw)
	if p == "" {
		return "", fmt.Errorf("manifest filesystem.path is required")
	}
	p = strings.ReplaceAll(p, "\\", "/")
	if strings.HasPrefix(p, "/") {
		return "", fmt.Errorf("manifest filesystem.path must be relative: %s", raw)
	}
	clean := path.Clean(p)
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return "", fmt.Errorf("manifest filesystem.path cannot escape repo root: %s", raw)
	}
	if clean == ".git" || strings.HasPrefix(clean, ".git/") {
		return "", fmt.Errorf("manifest filesystem.path cannot target .git: %s", raw)
	}
	return clean, nil
}

func filesystemAccessAllowsWrite(access []string) bool {
	if len(access) == 0 {
		return true
	}
	for _, a := range access {
		if strings.EqualFold(strings.TrimSpace(a), "write") {
			return true
		}
	}
	return false
}

func (o *Orchestrator) enforceManifestWritePolicy(ctx context.Context, ws *sandbox.Workspace, task *store.Task) error {
	policy, err := writePolicyFromManifest(task.Manifest)
	if err != nil {
		return fmt.Errorf("invalid task manifest: %w", err)
	}
	if !policy.enforced {
		return nil
	}

	changedPaths, err := o.changedRepoPaths(ctx, ws)
	if err != nil {
		return fmt.Errorf("filesystem policy check failed: %w", err)
	}
	if len(changedPaths) == 0 {
		return nil
	}

	violations := make([]string, 0, len(changedPaths))
	for _, p := range changedPaths {
		if !policy.allows(p) {
			violations = append(violations, p)
		}
	}
	if len(violations) == 0 {
		return nil
	}
	return fmt.Errorf("filesystem policy violation: unauthorized writes to %s", strings.Join(violations, ", "))
}

func (o *Orchestrator) changedRepoPaths(ctx context.Context, ws *sandbox.Workspace) ([]string, error) {
	cmds := []struct {
		args []string
		name string
	}{
		{args: []string{"git", "diff", "--name-only", "-z"}, name: "git diff"},
		{args: []string{"git", "diff", "--cached", "--name-only", "-z"}, name: "git diff --cached"},
		{args: []string{"git", "ls-files", "--others", "--exclude-standard", "-z"}, name: "git ls-files"},
	}

	seen := map[string]struct{}{}
	changed := make([]string, 0)
	for _, cmd := range cmds {
		res, err := o.sandbox.Exec(ctx, ws, cmd.args)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", cmd.name, err)
		}
		if res.ExitCode != 0 {
			msg := strings.TrimSpace(res.Stderr)
			if msg == "" {
				msg = strings.TrimSpace(res.Stdout)
			}
			if msg == "" {
				msg = "unknown error"
			}
			return nil, fmt.Errorf("%s exited %d: %s", cmd.name, res.ExitCode, msg)
		}

		for _, raw := range strings.Split(res.Stdout, "\x00") {
			p := normalizeChangedPath(raw)
			if p == "" {
				continue
			}
			if _, ok := seen[p]; ok {
				continue
			}
			seen[p] = struct{}{}
			changed = append(changed, p)
		}
	}
	sort.Strings(changed)
	return changed, nil
}

func normalizeChangedPath(raw string) string {
	p := strings.TrimSpace(raw)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, "/")
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

func (o *Orchestrator) recordPlanFailure(ctx context.Context, task *store.Task, plan AgentPlan, msg string) AgentResult {
	runID, _, err := o.createRunRecord(ctx, task, plan)
	if err != nil {
		return AgentResult{Index: plan.Index, ExitCode: 1, Output: fmt.Sprintf("%s; create run failed: %v", msg, err)}
	}
	o.store.UpdateRunState(ctx, runID, store.RunFailed, intPtr(1), msg)
	return AgentResult{Index: plan.Index, RunID: runID, ExitCode: 1, Output: msg}
}

func sharedHandoffBranch(taskID string, plans []AgentPlan) string {
	for _, p := range plans {
		if strings.TrimSpace(p.Branch) != "" {
			return p.Branch
		}
	}
	return fmt.Sprintf("orchestrate/%s/handoff", taskID)
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
	b := make([]byte, 16)
	binary.BigEndian.PutUint64(b[:8], uint64(time.Now().UnixMilli()))
	if _, err := rand.Read(b[8:]); err != nil {
		// Fall back to timestamp-derived bytes if CSPRNG is unavailable.
		binary.BigEndian.PutUint64(b[8:], uint64(time.Now().UnixNano()))
	}
	return hex.EncodeToString(b)
}
