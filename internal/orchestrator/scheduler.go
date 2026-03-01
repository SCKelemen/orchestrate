package orchestrator

import (
	"context"
	"log/slog"
	"runtime"
	"time"

	"golang.org/x/sync/semaphore"

	"github.com/SCKelemen/orchestrate/internal/schedule"
	"github.com/SCKelemen/orchestrate/internal/store"
)

// Scheduler polls the task queue and dispatches work to the orchestrator.
// It also checks for due schedules and creates tasks from them.
type Scheduler struct {
	store        *store.Store
	orchestrator *Orchestrator
	sem          *semaphore.Weighted
	pollInterval time.Duration
	logger       *slog.Logger
}

// SchedulerOpts configures the scheduler.
type SchedulerOpts struct {
	MaxConcurrent int
	PollInterval  time.Duration
}

// NewScheduler creates a new task scheduler.
func NewScheduler(s *store.Store, orch *Orchestrator, opts SchedulerOpts, logger *slog.Logger) *Scheduler {
	maxC := opts.MaxConcurrent
	if maxC <= 0 {
		maxC = runtime.NumCPU() / 2
		if maxC < 1 {
			maxC = 1
		}
	}
	poll := opts.PollInterval
	if poll <= 0 {
		poll = 5 * time.Second
	}
	return &Scheduler{
		store:        s,
		orchestrator: orch,
		sem:          semaphore.NewWeighted(int64(maxC)),
		pollInterval: poll,
		logger:       logger,
	}
}

// Run starts the scheduler loop. Blocks until ctx is cancelled.
func (sc *Scheduler) Run(ctx context.Context) error {
	sc.logger.Info("scheduler started", "pollInterval", sc.pollInterval)

	ticker := time.NewTicker(sc.pollInterval)
	defer ticker.Stop()

	// Clean expired sessions every 10 minutes.
	cleanTicker := time.NewTicker(10 * time.Minute)
	defer cleanTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			sc.logger.Info("scheduler stopping")
			return ctx.Err()
		case <-cleanTicker.C:
			sc.cleanExpiredSessions(ctx)
		case <-ticker.C:
			sc.checkSchedules(ctx)
			sc.poll(ctx)
		}
	}
}

func (sc *Scheduler) poll(ctx context.Context) {
	// Try to acquire a slot
	if !sc.sem.TryAcquire(1) {
		return // all slots busy
	}

	task, err := sc.store.DequeueTask(ctx)
	if err != nil {
		sc.sem.Release(1)
		sc.logger.Error("dequeue failed", "error", err)
		return
	}
	if task == nil {
		sc.sem.Release(1)
		return // queue empty
	}

	sc.logger.Info("dequeued task", "task", task.ID, "strategy", task.Strategy)

	// Execute in background
	go func() {
		defer sc.sem.Release(1)
		if err := sc.orchestrator.Execute(ctx, task); err != nil {
			sc.logger.Error("task execution failed", "task", task.ID, "error", err)
		}
	}()
}

func (sc *Scheduler) cleanExpiredSessions(ctx context.Context) {
	n, err := sc.store.CleanExpiredSessions(ctx)
	if err != nil {
		sc.logger.Error("clean expired sessions", "error", err)
		return
	}
	if n > 0 {
		sc.logger.Info("cleaned expired sessions", "count", n)
	}
}

// checkSchedules finds due schedules and creates tasks from them.
func (sc *Scheduler) checkSchedules(ctx context.Context) {
	now := time.Now().UTC()
	due, err := sc.store.DueSchedules(ctx, now)
	if err != nil {
		sc.logger.Error("check schedules failed", "error", err)
		return
	}

	for _, sched := range due {
		sc.logger.Info("schedule triggered", "schedule", sched.ID, "title", sched.Title)

		// Create a task from the schedule template
		taskID := newID()
		title := sched.Title
		if title == "" {
			title = "scheduled run"
		}
		_, err := sc.store.CreateTask(ctx, taskID, store.CreateTaskParams{
			OwnerUserID: sched.OwnerUserID,
			Agent:       sched.Agent,
			Title:       title + " (schedule/" + sched.ID + ")",
			Description: sched.Description,
			Prompt:      sched.Prompt,
			RepoURL:     sched.RepoURL,
			BaseRef:     sched.BaseRef,
			Strategy:    sched.Strategy,
			AgentCount:  sched.AgentCount,
			Image:       sched.Image,
			Manifest:    sched.Manifest,
		})
		if err != nil {
			sc.logger.Error("create scheduled task failed", "schedule", sched.ID, "error", err)
			continue
		}

		// Compute the next run time and advance the schedule
		spec, err := schedule.Parse(sched.ScheduleExpr)
		if err != nil {
			sc.logger.Error("parse schedule expr", "schedule", sched.ID, "error", err)
			continue
		}
		next := spec.Next(now)
		if next.IsZero() {
			sc.store.AdvanceSchedule(ctx, sched.ID, now, nil)
		} else {
			sc.store.AdvanceSchedule(ctx, sched.ID, now, &next)
		}

		sc.logger.Info("scheduled task created", "schedule", sched.ID, "task", taskID, "nextRun", next)
	}
}
