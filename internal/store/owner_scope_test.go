package store

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestOwnerColumnsExistAfterMigration(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "store.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	if !columnExists(t, s, "tasks", "owner_user_id") {
		t.Fatal("tasks.owner_user_id column missing")
	}
	if !columnExists(t, s, "tasks", "agent") {
		t.Fatal("tasks.agent column missing")
	}
	if !columnExists(t, s, "schedules", "owner_user_id") {
		t.Fatal("schedules.owner_user_id column missing")
	}
	if !columnExists(t, s, "schedules", "agent") {
		t.Fatal("schedules.agent column missing")
	}
}

func TestTaskOwnerFilter(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "store.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	if _, err := s.CreateTask(ctx, "t1", CreateTaskParams{
		OwnerUserID: "u1",
		Prompt:      "p1",
		RepoURL:     "https://example.com/repo.git",
	}); err != nil {
		t.Fatalf("create task t1: %v", err)
	}
	if _, err := s.CreateTask(ctx, "t2", CreateTaskParams{
		OwnerUserID: "u2",
		Prompt:      "p2",
		RepoURL:     "https://example.com/repo.git",
	}); err != nil {
		t.Fatalf("create task t2: %v", err)
	}

	tasks, err := s.ListTasks(ctx, ListTasksParams{OwnerUserID: "u1"})
	if err != nil {
		t.Fatalf("list tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("tasks len=%d want=1", len(tasks))
	}
	if tasks[0].OwnerUserID != "u1" {
		t.Fatalf("owner=%q want=u1", tasks[0].OwnerUserID)
	}
	if tasks[0].Agent != "claude" {
		t.Fatalf("agent=%q want=claude", tasks[0].Agent)
	}
}

func TestScheduleOwnerFilter(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "store.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	next := time.Now().UTC().Add(time.Minute).Format(time.RFC3339)
	if _, err := s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		OwnerUserID:  "u1",
		ScheduleExpr: "0 * * * *",
		ScheduleType: "CRON",
		Prompt:       "p1",
		RepoURL:      "https://example.com/repo.git",
		NextRunTime:  next,
	}); err != nil {
		t.Fatalf("create schedule s1: %v", err)
	}
	if _, err := s.CreateSchedule(ctx, "s2", CreateScheduleParams{
		OwnerUserID:  "u2",
		ScheduleExpr: "0 * * * *",
		ScheduleType: "CRON",
		Prompt:       "p2",
		RepoURL:      "https://example.com/repo.git",
		NextRunTime:  next,
	}); err != nil {
		t.Fatalf("create schedule s2: %v", err)
	}

	schedules, err := s.ListSchedules(ctx, ListSchedulesParams{OwnerUserID: "u1"})
	if err != nil {
		t.Fatalf("list schedules: %v", err)
	}
	if len(schedules) != 1 {
		t.Fatalf("schedules len=%d want=1", len(schedules))
	}
	if schedules[0].OwnerUserID != "u1" {
		t.Fatalf("owner=%q want=u1", schedules[0].OwnerUserID)
	}
	if schedules[0].Agent != "claude" {
		t.Fatalf("agent=%q want=claude", schedules[0].Agent)
	}
}

func columnExists(t *testing.T, s *Store, table, column string) bool {
	t.Helper()
	rows, err := s.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		t.Fatalf("pragma table_info(%s): %v", table, err)
	}
	defer rows.Close()

	var (
		cid     int
		name    string
		typ     string
		notNull int
		defVal  any
		pk      int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defVal, &pk); err != nil {
			t.Fatalf("scan pragma row: %v", err)
		}
		if name == column {
			return true
		}
	}
	return false
}

func TestMigrationAddsAgentAndOwnerColumnsToLegacySchema(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	_, err = db.Exec(`
		CREATE TABLE tasks (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			prompt TEXT NOT NULL,
			repo_url TEXT NOT NULL,
			base_ref TEXT NOT NULL DEFAULT 'main',
			strategy TEXT NOT NULL DEFAULT 'IMPLEMENT',
			agent_count INTEGER NOT NULL DEFAULT 1,
			priority INTEGER NOT NULL DEFAULT 0,
			state TEXT NOT NULL DEFAULT 'QUEUED',
			image TEXT NOT NULL DEFAULT 'orchestrate-agent:latest',
			create_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
			start_time TEXT,
			end_time TEXT
		);
		CREATE TABLE schedules (
			id TEXT PRIMARY KEY,
			title TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			schedule_expr TEXT NOT NULL,
			schedule_type TEXT NOT NULL DEFAULT 'CRON',
			prompt TEXT NOT NULL,
			repo_url TEXT NOT NULL,
			base_ref TEXT NOT NULL DEFAULT 'main',
			strategy TEXT NOT NULL DEFAULT 'IMPLEMENT',
			agent_count INTEGER NOT NULL DEFAULT 1,
			image TEXT NOT NULL DEFAULT 'orchestrate-agent:latest',
			state TEXT NOT NULL DEFAULT 'ACTIVE',
			last_run_time TEXT,
			next_run_time TEXT,
			run_count INTEGER NOT NULL DEFAULT 0,
			max_runs INTEGER NOT NULL DEFAULT 0,
			create_time TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		);
		INSERT INTO tasks (id, prompt, repo_url) VALUES ('t-legacy', 'p', 'https://example.com/repo.git');
		INSERT INTO schedules (id, schedule_expr, prompt, repo_url) VALUES ('s-legacy', '0 * * * *', 'p', 'https://example.com/repo.git');
	`)
	if err != nil {
		t.Fatalf("seed legacy db: %v", err)
	}
	_ = db.Close()

	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open migrated store: %v", err)
	}
	defer s.Close()

	task, err := s.GetTask(context.Background(), "t-legacy")
	if err != nil || task == nil {
		t.Fatalf("get migrated task err=%v task=%v", err, task)
	}
	if task.OwnerUserID != "system" || task.Agent != "claude" {
		t.Fatalf("task defaults owner=%q agent=%q", task.OwnerUserID, task.Agent)
	}

	sc, err := s.GetSchedule(context.Background(), "s-legacy")
	if err != nil || sc == nil {
		t.Fatalf("get migrated schedule err=%v schedule=%v", err, sc)
	}
	if sc.OwnerUserID != "system" || sc.Agent != "claude" {
		t.Fatalf("schedule defaults owner=%q agent=%q", sc.OwnerUserID, sc.Agent)
	}
}
