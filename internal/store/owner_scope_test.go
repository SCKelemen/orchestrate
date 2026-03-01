package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
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
	if !columnExists(t, s, "schedules", "owner_user_id") {
		t.Fatal("schedules.owner_user_id column missing")
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
