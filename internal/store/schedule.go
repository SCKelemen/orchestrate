package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ScheduleState represents the lifecycle state of a schedule.
type ScheduleState string

const (
	ScheduleActive ScheduleState = "ACTIVE"
	SchedulePaused ScheduleState = "PAUSED"
)

// Schedule represents a recurring task template.
type Schedule struct {
	ID           string        `json:"name"`
	OwnerUserID  string        `json:"ownerUserId"`
	Title        string        `json:"title"`
	Description  string        `json:"description"`
	ScheduleExpr string        `json:"scheduleExpr"`
	ScheduleType string        `json:"scheduleType"`
	Prompt       string        `json:"prompt"`
	RepoURL      string        `json:"repoUrl"`
	BaseRef      string        `json:"baseRef"`
	Strategy     Strategy      `json:"strategy"`
	AgentCount   int           `json:"agentCount"`
	Image        string        `json:"image"`
	State        ScheduleState `json:"state"`
	LastRunTime  *string       `json:"lastRunTime"`
	NextRunTime  *string       `json:"nextRunTime"`
	RunCount     int           `json:"runCount"`
	MaxRuns      int           `json:"maxRuns"`
	CreateTime   string        `json:"createTime"`
}

// CreateScheduleParams holds parameters for creating a new schedule.
type CreateScheduleParams struct {
	OwnerUserID  string
	Title        string
	Description  string
	ScheduleExpr string
	ScheduleType string
	Prompt       string
	RepoURL      string
	BaseRef      string
	Strategy     Strategy
	AgentCount   int
	Image        string
	NextRunTime  string
	MaxRuns      int
}

// CreateSchedule inserts a new schedule and returns it.
func (s *Store) CreateSchedule(ctx context.Context, id string, p CreateScheduleParams) (*Schedule, error) {
	if p.OwnerUserID == "" {
		p.OwnerUserID = "system"
	}
	if p.BaseRef == "" {
		p.BaseRef = "main"
	}
	if p.Strategy == "" {
		p.Strategy = StrategyImplement
	}
	if p.AgentCount < 1 {
		p.AgentCount = 1
	}
	if p.Image == "" {
		p.Image = "orchestrate-agent:latest"
	}

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO schedules (id, owner_user_id, title, description, schedule_expr, schedule_type,
			prompt, repo_url, base_ref, strategy, agent_count, image, next_run_time, max_runs)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.OwnerUserID, p.Title, p.Description, p.ScheduleExpr, p.ScheduleType,
		p.Prompt, p.RepoURL, p.BaseRef, string(p.Strategy), p.AgentCount, p.Image,
		p.NextRunTime, p.MaxRuns,
	)
	if err != nil {
		return nil, fmt.Errorf("insert schedule: %w", err)
	}
	return s.GetSchedule(ctx, id)
}

// GetSchedule retrieves a schedule by ID.
func (s *Store) GetSchedule(ctx context.Context, id string) (*Schedule, error) {
	sc := &Schedule{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, title, description, schedule_expr, schedule_type,
		       prompt, repo_url, base_ref, strategy, agent_count, image,
		       state, last_run_time, next_run_time, run_count, max_runs, create_time
		FROM schedules WHERE id = ?`, id,
	).Scan(
		&sc.ID, &sc.OwnerUserID, &sc.Title, &sc.Description, &sc.ScheduleExpr, &sc.ScheduleType,
		&sc.Prompt, &sc.RepoURL, &sc.BaseRef, &sc.Strategy, &sc.AgentCount, &sc.Image,
		&sc.State, &sc.LastRunTime, &sc.NextRunTime, &sc.RunCount, &sc.MaxRuns, &sc.CreateTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get schedule: %w", err)
	}
	return sc, nil
}

// ListSchedules returns all schedules, optionally filtered by state.
type ListSchedulesParams struct {
	OwnerUserID string
	State       ScheduleState
	PageSize    int
	PageToken   string
}

func (s *Store) ListSchedules(ctx context.Context, p ListSchedulesParams) ([]*Schedule, error) {
	if p.PageSize <= 0 || p.PageSize > 100 {
		p.PageSize = 20
	}

	query := `SELECT id, owner_user_id, title, description, schedule_expr, schedule_type,
	                 prompt, repo_url, base_ref, strategy, agent_count, image,
	                 state, last_run_time, next_run_time, run_count, max_runs, create_time
	          FROM schedules`
	args := []any{}

	var where []string
	if p.OwnerUserID != "" {
		where = append(where, "owner_user_id = ?")
		args = append(args, p.OwnerUserID)
	}
	if p.State != "" {
		where = append(where, "state = ?")
		args = append(args, string(p.State))
	}
	if p.PageToken != "" {
		where = append(where, "id > ?")
		args = append(args, p.PageToken)
	}
	if len(where) > 0 {
		query += " WHERE "
		for i, w := range where {
			if i > 0 {
				query += " AND "
			}
			query += w
		}
	}
	query += " ORDER BY create_time ASC LIMIT ?"
	args = append(args, p.PageSize+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*Schedule
	for rows.Next() {
		sc := &Schedule{}
		if err := rows.Scan(
			&sc.ID, &sc.OwnerUserID, &sc.Title, &sc.Description, &sc.ScheduleExpr, &sc.ScheduleType,
			&sc.Prompt, &sc.RepoURL, &sc.BaseRef, &sc.Strategy, &sc.AgentCount, &sc.Image,
			&sc.State, &sc.LastRunTime, &sc.NextRunTime, &sc.RunCount, &sc.MaxRuns, &sc.CreateTime,
		); err != nil {
			return nil, fmt.Errorf("scan schedule: %w", err)
		}
		schedules = append(schedules, sc)
	}
	return schedules, rows.Err()
}

// DueSchedules returns active schedules whose next_run_time is at or before the given time.
func (s *Store) DueSchedules(ctx context.Context, now time.Time) ([]*Schedule, error) {
	nowStr := now.UTC().Format(time.RFC3339)
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, owner_user_id, title, description, schedule_expr, schedule_type,
		       prompt, repo_url, base_ref, strategy, agent_count, image,
		       state, last_run_time, next_run_time, run_count, max_runs, create_time
		FROM schedules
		WHERE state = 'ACTIVE' AND next_run_time IS NOT NULL AND next_run_time <= ?
		ORDER BY next_run_time ASC`, nowStr,
	)
	if err != nil {
		return nil, fmt.Errorf("due schedules: %w", err)
	}
	defer rows.Close()

	var schedules []*Schedule
	for rows.Next() {
		sc := &Schedule{}
		if err := rows.Scan(
			&sc.ID, &sc.OwnerUserID, &sc.Title, &sc.Description, &sc.ScheduleExpr, &sc.ScheduleType,
			&sc.Prompt, &sc.RepoURL, &sc.BaseRef, &sc.Strategy, &sc.AgentCount, &sc.Image,
			&sc.State, &sc.LastRunTime, &sc.NextRunTime, &sc.RunCount, &sc.MaxRuns, &sc.CreateTime,
		); err != nil {
			return nil, fmt.Errorf("scan due schedule: %w", err)
		}
		schedules = append(schedules, sc)
	}
	return schedules, rows.Err()
}

// AdvanceSchedule updates a schedule after a run: increments run_count, sets last_run_time,
// computes next_run_time, and pauses if max_runs is reached.
func (s *Store) AdvanceSchedule(ctx context.Context, id string, lastRun time.Time, nextRun *time.Time) error {
	nowStr := lastRun.UTC().Format(time.RFC3339)
	var nextStr *string
	if nextRun != nil {
		v := nextRun.UTC().Format(time.RFC3339)
		nextStr = &v
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE schedules SET
			run_count = run_count + 1,
			last_run_time = ?,
			next_run_time = ?,
			state = CASE
				WHEN max_runs > 0 AND run_count + 1 >= max_runs THEN 'PAUSED'
				ELSE state
			END
		WHERE id = ?`, nowStr, nextStr, id,
	)
	if err != nil {
		return fmt.Errorf("advance schedule: %w", err)
	}
	return nil
}

// UpdateScheduleState changes a schedule's state (ACTIVE/PAUSED).
func (s *Store) UpdateScheduleState(ctx context.Context, id string, state ScheduleState) error {
	_, err := s.db.ExecContext(ctx, `UPDATE schedules SET state = ? WHERE id = ?`, string(state), id)
	if err != nil {
		return fmt.Errorf("update schedule state: %w", err)
	}
	return nil
}

// UpdateSchedule updates mutable schedule fields.
func (s *Store) UpdateSchedule(ctx context.Context, id string, title, description *string) error {
	if title != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE schedules SET title = ? WHERE id = ?`, *title, id); err != nil {
			return fmt.Errorf("update schedule title: %w", err)
		}
	}
	if description != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE schedules SET description = ? WHERE id = ?`, *description, id); err != nil {
			return fmt.Errorf("update schedule description: %w", err)
		}
	}
	return nil
}

// ResumeSchedule reactivates a paused schedule with a new next run time.
func (s *Store) ResumeSchedule(ctx context.Context, id string, nextRun time.Time) error {
	nextStr := nextRun.UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx,
		`UPDATE schedules SET state = 'ACTIVE', next_run_time = ? WHERE id = ?`,
		nextStr, id)
	if err != nil {
		return fmt.Errorf("resume schedule: %w", err)
	}
	return nil
}

// DeleteSchedule removes a schedule by ID.
func (s *Store) DeleteSchedule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("schedule not found: %s", id)
	}
	return nil
}
