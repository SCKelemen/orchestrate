package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// RunState represents the lifecycle state of an agent run.
type RunState string

const (
	RunPending   RunState = "PENDING"
	RunRunning   RunState = "RUNNING"
	RunSucceeded RunState = "SUCCEEDED"
	RunFailed    RunState = "FAILED"
	RunCancelled RunState = "CANCELLED"
)

// Run represents a single agent execution within a task.
type Run struct {
	ID         string   `json:"name"`
	TaskID     string   `json:"taskId"`
	AgentIndex int      `json:"agentIndex"`
	Branch     string   `json:"branch"`
	State      RunState `json:"state"`
	ExitCode   *int     `json:"exitCode"`
	Output     string   `json:"output"`
	LogPath    string   `json:"logPath"`
	CreateTime string   `json:"createTime"`
	StartTime  *string  `json:"startTime"`
	EndTime    *string  `json:"endTime"`
}

// CreateRunParams holds parameters for creating a new run.
type CreateRunParams struct {
	TaskID     string
	AgentIndex int
	Branch     string
	LogPath    string
}

// CreateRun inserts a new run and returns it.
func (s *Store) CreateRun(ctx context.Context, id string, p CreateRunParams) (*Run, error) {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO runs (id, task_id, agent_index, branch, log_path)
		VALUES (?, ?, ?, ?, ?)`,
		id, p.TaskID, p.AgentIndex, p.Branch, p.LogPath,
	)
	if err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}
	return s.GetRun(ctx, id)
}

// GetRun retrieves a run by ID.
func (s *Store) GetRun(ctx context.Context, id string) (*Run, error) {
	r := &Run{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, task_id, agent_index, branch, state, exit_code,
		       output, log_path, create_time, start_time, end_time
		FROM runs WHERE id = ?`, id,
	).Scan(
		&r.ID, &r.TaskID, &r.AgentIndex, &r.Branch, &r.State, &r.ExitCode,
		&r.Output, &r.LogPath, &r.CreateTime, &r.StartTime, &r.EndTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	return r, nil
}

// ListRuns returns all runs for a given task.
func (s *Store) ListRuns(ctx context.Context, taskID string) ([]*Run, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, task_id, agent_index, branch, state, exit_code,
		       output, log_path, create_time, start_time, end_time
		FROM runs WHERE task_id = ?
		ORDER BY agent_index ASC`, taskID,
	)
	if err != nil {
		return nil, fmt.Errorf("list runs: %w", err)
	}
	defer rows.Close()

	var runs []*Run
	for rows.Next() {
		r := &Run{}
		if err := rows.Scan(
			&r.ID, &r.TaskID, &r.AgentIndex, &r.Branch, &r.State, &r.ExitCode,
			&r.Output, &r.LogPath, &r.CreateTime, &r.StartTime, &r.EndTime,
		); err != nil {
			return nil, fmt.Errorf("scan run: %w", err)
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// UpdateRunState transitions a run to a new state.
func (s *Store) UpdateRunState(ctx context.Context, id string, state RunState, exitCode *int, output string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var startTime *string
	if state == RunRunning {
		startTime = &now
	}
	var endTime *string
	if state == RunSucceeded || state == RunFailed || state == RunCancelled {
		endTime = &now
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE runs SET state = ?,
			exit_code = COALESCE(?, exit_code),
			output = CASE WHEN ? != '' THEN ? ELSE output END,
			start_time = COALESCE(?, start_time),
			end_time = COALESCE(?, end_time)
		WHERE id = ?`,
		string(state), exitCode, output, output, startTime, endTime, id,
	)
	if err != nil {
		return fmt.Errorf("update run state: %w", err)
	}
	return nil
}
