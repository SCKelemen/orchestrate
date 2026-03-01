package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// TaskState represents the lifecycle state of a task.
type TaskState string

const (
	TaskQueued    TaskState = "QUEUED"
	TaskRunning   TaskState = "RUNNING"
	TaskSucceeded TaskState = "SUCCEEDED"
	TaskFailed    TaskState = "FAILED"
	TaskCancelled TaskState = "CANCELLED"
)

// Strategy represents the execution strategy for a task.
type Strategy string

const (
	StrategyImplement   Strategy = "IMPLEMENT"
	StrategyInvestigate Strategy = "INVESTIGATE"
	StrategyCompete     Strategy = "COMPETE"
	StrategyBatch       Strategy = "BATCH"
)

// Task represents a unit of work for one or more agents.
type Task struct {
	ID          string    `json:"name"`
	OwnerUserID string    `json:"ownerUserId"`
	Agent       string    `json:"agent"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Prompt      string    `json:"prompt"`
	RepoURL     string    `json:"repoUrl"`
	BaseRef     string    `json:"baseRef"`
	Strategy    Strategy  `json:"strategy"`
	AgentCount  int       `json:"agentCount"`
	Priority    int       `json:"priority"`
	State       TaskState `json:"state"`
	Image       string    `json:"image"`
	CreateTime  string    `json:"createTime"`
	StartTime   *string   `json:"startTime"`
	EndTime     *string   `json:"endTime"`
}

// CreateTaskParams holds parameters for creating a new task.
type CreateTaskParams struct {
	OwnerUserID string
	Agent       string
	Title       string
	Description string
	Prompt      string
	RepoURL     string
	BaseRef     string
	Strategy    Strategy
	AgentCount  int
	Priority    int
	Image       string
}

// CreateTask inserts a new task and returns it.
func (s *Store) CreateTask(ctx context.Context, id string, p CreateTaskParams) (*Task, error) {
	if p.OwnerUserID == "" {
		p.OwnerUserID = "system"
	}
	if p.Agent == "" {
		p.Agent = "claude"
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
		INSERT INTO tasks (id, owner_user_id, agent, title, description, prompt, repo_url, base_ref, strategy, agent_count, priority, image)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.OwnerUserID, p.Agent, p.Title, p.Description, p.Prompt, p.RepoURL, p.BaseRef,
		string(p.Strategy), p.AgentCount, p.Priority, p.Image,
	)
	if err != nil {
		return nil, fmt.Errorf("insert task: %w", err)
	}
	return s.GetTask(ctx, id)
}

// GetTask retrieves a task by ID.
func (s *Store) GetTask(ctx context.Context, id string) (*Task, error) {
	t := &Task{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, owner_user_id, agent, title, description, prompt, repo_url, base_ref,
		       strategy, agent_count, priority, state, image,
		       create_time, start_time, end_time
		FROM tasks WHERE id = ?`, id,
	).Scan(
		&t.ID, &t.OwnerUserID, &t.Agent, &t.Title, &t.Description, &t.Prompt, &t.RepoURL, &t.BaseRef,
		&t.Strategy, &t.AgentCount, &t.Priority, &t.State, &t.Image,
		&t.CreateTime, &t.StartTime, &t.EndTime,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get task: %w", err)
	}
	return t, nil
}

// ListTasksParams holds filter/pagination parameters.
type ListTasksParams struct {
	OwnerUserID string
	State       TaskState
	PageSize    int
	PageToken   string // task ID to start after
}

// ListTasks returns tasks matching the given filter, ordered by priority desc then create_time asc.
func (s *Store) ListTasks(ctx context.Context, p ListTasksParams) ([]*Task, error) {
	if p.PageSize <= 0 || p.PageSize > 100 {
		p.PageSize = 20
	}

	query := `SELECT id, owner_user_id, agent, title, description, prompt, repo_url, base_ref,
	                 strategy, agent_count, priority, state, image,
	                 create_time, start_time, end_time
	          FROM tasks`
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
	query += " ORDER BY priority DESC, create_time ASC LIMIT ?"
	args = append(args, p.PageSize+1)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()

	var tasks []*Task
	for rows.Next() {
		t := &Task{}
		if err := rows.Scan(
			&t.ID, &t.OwnerUserID, &t.Agent, &t.Title, &t.Description, &t.Prompt, &t.RepoURL, &t.BaseRef,
			&t.Strategy, &t.AgentCount, &t.Priority, &t.State, &t.Image,
			&t.CreateTime, &t.StartTime, &t.EndTime,
		); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, t)
	}
	return tasks, rows.Err()
}

// DequeueTask atomically claims the next queued task by transitioning it to RUNNING.
func (s *Store) DequeueTask(ctx context.Context) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var id string
	err = tx.QueryRowContext(ctx, `
		SELECT id FROM tasks
		WHERE state = 'QUEUED'
		ORDER BY priority DESC, create_time ASC
		LIMIT 1`,
	).Scan(&id)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("select queued: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	_, err = tx.ExecContext(ctx, `
		UPDATE tasks SET state = 'RUNNING', start_time = ?
		WHERE id = ? AND state = 'QUEUED'`, now, id,
	)
	if err != nil {
		return nil, fmt.Errorf("claim task: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return s.GetTask(ctx, id)
}

// UpdateTaskState transitions a task to a new state.
func (s *Store) UpdateTaskState(ctx context.Context, id string, state TaskState) error {
	var endTime *string
	if state == TaskSucceeded || state == TaskFailed || state == TaskCancelled {
		now := time.Now().UTC().Format(time.RFC3339)
		endTime = &now
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE tasks SET state = ?, end_time = COALESCE(?, end_time)
		WHERE id = ?`, string(state), endTime, id,
	)
	if err != nil {
		return fmt.Errorf("update task state: %w", err)
	}
	return nil
}

// UpdateTask updates mutable task fields.
func (s *Store) UpdateTask(ctx context.Context, id string, title, description *string, priority *int) error {
	if title != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET title = ? WHERE id = ?`, *title, id); err != nil {
			return fmt.Errorf("update title: %w", err)
		}
	}
	if description != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET description = ? WHERE id = ?`, *description, id); err != nil {
			return fmt.Errorf("update description: %w", err)
		}
	}
	if priority != nil {
		if _, err := s.db.ExecContext(ctx, `UPDATE tasks SET priority = ? WHERE id = ?`, *priority, id); err != nil {
			return fmt.Errorf("update priority: %w", err)
		}
	}
	return nil
}

// DeleteTask removes a task by ID.
func (s *Store) DeleteTask(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM tasks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("task not found: %s", id)
	}
	return nil
}
