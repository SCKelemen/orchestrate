package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// --- Task ---

func TestTaskCreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	task, err := s.CreateTask(ctx, "t1", CreateTaskParams{
		Prompt:  "do something",
		RepoURL: "https://github.com/test/repo",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if task.ID != "t1" {
		t.Errorf("id = %q, want t1", task.ID)
	}
	if task.State != TaskQueued {
		t.Errorf("state = %q, want QUEUED", task.State)
	}
	if task.BaseRef != "main" {
		t.Errorf("baseRef = %q, want main", task.BaseRef)
	}
	if task.Strategy != StrategyImplement {
		t.Errorf("strategy = %q, want IMPLEMENT", task.Strategy)
	}
	if task.AgentCount != 1 {
		t.Errorf("agentCount = %d, want 1", task.AgentCount)
	}
	if task.Image != "orchestrate-agent:latest" {
		t.Errorf("image = %q, want orchestrate-agent:latest", task.Image)
	}

	got, err := s.GetTask(ctx, "t1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.Prompt != "do something" {
		t.Errorf("prompt = %q", got.Prompt)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	task, err := s.GetTask(context.Background(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil, got %+v", task)
	}
}

func TestListTasksPagination(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "t" + string(rune('a'+i))
		_, err := s.CreateTask(ctx, id, CreateTaskParams{
			Prompt:  "p",
			RepoURL: "https://github.com/test/repo",
		})
		if err != nil {
			t.Fatalf("create %s: %v", id, err)
		}
	}

	tasks, err := s.ListTasks(ctx, ListTasksParams{PageSize: 3})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// PageSize+1 is fetched for nextPageToken; we get up to 4
	if len(tasks) < 3 {
		t.Fatalf("got %d tasks, want at least 3", len(tasks))
	}
}

func TestListTasksStateFilter(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "q1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	s.CreateTask(ctx, "q2", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	s.UpdateTaskState(ctx, "q2", TaskFailed)

	tasks, err := s.ListTasks(ctx, ListTasksParams{State: TaskQueued})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("got %d tasks, want 1", len(tasks))
	}
	if tasks[0].ID != "q1" {
		t.Errorf("got id %q, want q1", tasks[0].ID)
	}
}

func TestDequeueTaskFIFOAndPriority(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "low", CreateTaskParams{Prompt: "p", RepoURL: "r", Priority: 0})
	s.CreateTask(ctx, "high", CreateTaskParams{Prompt: "p", RepoURL: "r", Priority: 10})

	task, err := s.DequeueTask(ctx)
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if task == nil {
		t.Fatal("dequeue returned nil")
	}
	if task.ID != "high" {
		t.Errorf("dequeued %q, want high (higher priority)", task.ID)
	}
	if task.State != TaskRunning {
		t.Errorf("state = %q, want RUNNING", task.State)
	}
}

func TestDequeueTaskEmpty(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	task, err := s.DequeueTask(context.Background())
	if err != nil {
		t.Fatalf("dequeue: %v", err)
	}
	if task != nil {
		t.Fatalf("expected nil, got %+v", task)
	}
}

func TestUpdateTaskState(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	if err := s.UpdateTaskState(ctx, "t1", TaskFailed); err != nil {
		t.Fatalf("update state: %v", err)
	}
	task, _ := s.GetTask(ctx, "t1")
	if task.State != TaskFailed {
		t.Errorf("state = %q, want FAILED", task.State)
	}
	if task.EndTime == nil {
		t.Error("end_time should be set for terminal state")
	}
}

func TestUpdateTask(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	title := "new title"
	prio := 5
	if err := s.UpdateTask(ctx, "t1", &title, nil, &prio); err != nil {
		t.Fatalf("update: %v", err)
	}
	task, _ := s.GetTask(ctx, "t1")
	if task.Title != "new title" {
		t.Errorf("title = %q", task.Title)
	}
	if task.Priority != 5 {
		t.Errorf("priority = %d", task.Priority)
	}
}

func TestDeleteTask(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	if err := s.DeleteTask(ctx, "t1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	task, _ := s.GetTask(ctx, "t1")
	if task != nil {
		t.Error("task should be deleted")
	}
}

func TestDeleteTaskNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	err := s.DeleteTask(context.Background(), "nope")
	if err == nil {
		t.Fatal("expected error for missing task")
	}
}

// --- Run ---

func TestRunCreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	run, err := s.CreateRun(ctx, "r1", CreateRunParams{
		TaskID:     "t1",
		AgentIndex: 0,
		Branch:     "agent-0",
		LogPath:    "/tmp/log.txt",
	})
	if err != nil {
		t.Fatalf("create run: %v", err)
	}
	if run.State != RunPending {
		t.Errorf("state = %q, want PENDING", run.State)
	}

	got, err := s.GetRun(ctx, "r1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TaskID != "t1" {
		t.Errorf("taskID = %q", got.TaskID)
	}
}

func TestListRunsOrdered(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	s.CreateRun(ctx, "r2", CreateRunParams{TaskID: "t1", AgentIndex: 2, Branch: "b2"})
	s.CreateRun(ctx, "r0", CreateRunParams{TaskID: "t1", AgentIndex: 0, Branch: "b0"})
	s.CreateRun(ctx, "r1", CreateRunParams{TaskID: "t1", AgentIndex: 1, Branch: "b1"})

	runs, err := s.ListRuns(ctx, "t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("got %d runs, want 3", len(runs))
	}
	for i, r := range runs {
		if r.AgentIndex != i {
			t.Errorf("runs[%d].AgentIndex = %d, want %d", i, r.AgentIndex, i)
		}
	}
}

func TestUpdateRunState(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	s.CreateRun(ctx, "r1", CreateRunParams{TaskID: "t1", AgentIndex: 0, Branch: "b"})

	if err := s.UpdateRunState(ctx, "r1", RunRunning, nil, ""); err != nil {
		t.Fatalf("update to running: %v", err)
	}
	r, _ := s.GetRun(ctx, "r1")
	if r.State != RunRunning {
		t.Errorf("state = %q, want RUNNING", r.State)
	}
	if r.StartTime == nil {
		t.Error("start_time should be set")
	}

	exitCode := 0
	if err := s.UpdateRunState(ctx, "r1", RunSucceeded, &exitCode, "done"); err != nil {
		t.Fatalf("update to succeeded: %v", err)
	}
	r, _ = s.GetRun(ctx, "r1")
	if r.State != RunSucceeded {
		t.Errorf("state = %q, want SUCCEEDED", r.State)
	}
	if r.ExitCode == nil || *r.ExitCode != 0 {
		t.Errorf("exitCode = %v", r.ExitCode)
	}
	if r.Output != "done" {
		t.Errorf("output = %q", r.Output)
	}
}

// --- Schedule ---

func TestScheduleCreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	sc, err := s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 0 * * *",
		ScheduleType: "CRON",
		Prompt:       "daily task",
		RepoURL:      "https://github.com/test/repo",
		NextRunTime:  next,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sc.State != ScheduleActive {
		t.Errorf("state = %q, want ACTIVE", sc.State)
	}

	got, _ := s.GetSchedule(ctx, "s1")
	if got.ScheduleExpr != "0 0 * * *" {
		t.Errorf("scheduleExpr = %q", got.ScheduleExpr)
	}
}

func TestListSchedules(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})
	s.CreateSchedule(ctx, "s2", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})

	list, err := s.ListSchedules(ctx, ListSchedulesParams{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d, want 2", len(list))
	}
}

func TestDueSchedules(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	past := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)

	s.CreateSchedule(ctx, "due", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: past,
	})
	s.CreateSchedule(ctx, "notdue", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: future,
	})

	due, err := s.DueSchedules(ctx, time.Now())
	if err != nil {
		t.Fatalf("due: %v", err)
	}
	if len(due) != 1 {
		t.Fatalf("got %d due, want 1", len(due))
	}
	if due[0].ID != "due" {
		t.Errorf("id = %q, want due", due[0].ID)
	}
}

func TestAdvanceSchedule(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})

	now := time.Now().UTC()
	nextRun := now.Add(time.Hour)
	if err := s.AdvanceSchedule(ctx, "s1", now, &nextRun); err != nil {
		t.Fatalf("advance: %v", err)
	}

	sc, _ := s.GetSchedule(ctx, "s1")
	if sc.RunCount != 1 {
		t.Errorf("run_count = %d, want 1", sc.RunCount)
	}
	if sc.LastRunTime == nil {
		t.Error("last_run_time should be set")
	}
}

func TestAdvanceScheduleMaxRunsPauses(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next, MaxRuns: 1,
	})

	now := time.Now().UTC()
	nextRun := now.Add(time.Hour)
	s.AdvanceSchedule(ctx, "s1", now, &nextRun)

	sc, _ := s.GetSchedule(ctx, "s1")
	if sc.State != SchedulePaused {
		t.Errorf("state = %q, want PAUSED after max_runs reached", sc.State)
	}
}

func TestUpdateScheduleState(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})

	if err := s.UpdateScheduleState(ctx, "s1", SchedulePaused); err != nil {
		t.Fatalf("update state: %v", err)
	}
	sc, _ := s.GetSchedule(ctx, "s1")
	if sc.State != SchedulePaused {
		t.Errorf("state = %q, want PAUSED", sc.State)
	}
}

func TestResumeSchedule(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})
	s.UpdateScheduleState(ctx, "s1", SchedulePaused)

	newNext := time.Now().Add(2 * time.Hour).UTC()
	if err := s.ResumeSchedule(ctx, "s1", newNext); err != nil {
		t.Fatalf("resume: %v", err)
	}
	sc, _ := s.GetSchedule(ctx, "s1")
	if sc.State != ScheduleActive {
		t.Errorf("state = %q, want ACTIVE", sc.State)
	}
}

func TestUpdateSchedule(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})

	title := "updated"
	if err := s.UpdateSchedule(ctx, "s1", &title, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	sc, _ := s.GetSchedule(ctx, "s1")
	if sc.Title != "updated" {
		t.Errorf("title = %q", sc.Title)
	}
}

func TestDeleteSchedule(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})

	if err := s.DeleteSchedule(ctx, "s1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	sc, _ := s.GetSchedule(ctx, "s1")
	if sc != nil {
		t.Error("schedule should be deleted")
	}

	err := s.DeleteSchedule(ctx, "s1")
	if err == nil {
		t.Error("second delete should error")
	}
}

// --- User ---

func TestUserCreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "u1", CreateUserParams{
		DisplayName: "Alice",
		Email:       "alice@example.com",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if u.DisplayName != "Alice" {
		t.Errorf("displayName = %q", u.DisplayName)
	}
	if u.State != "ACTIVE" {
		t.Errorf("state = %q, want ACTIVE", u.State)
	}

	got, _ := s.GetUser(ctx, "u1")
	if got.Email != "alice@example.com" {
		t.Errorf("email = %q", got.Email)
	}
}

func TestGetUserByEmail(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{
		DisplayName: "Alice",
		Email:       "alice@example.com",
	})

	u, err := s.GetUserByEmail(ctx, "alice@example.com")
	if err != nil {
		t.Fatalf("get by email: %v", err)
	}
	if u.ID != "u1" {
		t.Errorf("id = %q, want u1", u.ID)
	}

	missing, _ := s.GetUserByEmail(ctx, "nobody@example.com")
	if missing != nil {
		t.Error("expected nil for missing email")
	}
}

func TestUpdateUser(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "Alice", Email: "a@x.com"})
	name := "Bob"
	if err := s.UpdateUser(ctx, "u1", &name, nil); err != nil {
		t.Fatalf("update: %v", err)
	}
	u, _ := s.GetUser(ctx, "u1")
	if u.DisplayName != "Bob" {
		t.Errorf("displayName = %q", u.DisplayName)
	}
}

func TestDeleteUser(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	if err := s.DeleteUser(ctx, "u1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	u, _ := s.GetUser(ctx, "u1")
	if u != nil {
		t.Error("user should be deleted")
	}

	err := s.DeleteUser(ctx, "u1")
	if err == nil {
		t.Error("second delete should error")
	}
}

func TestListUsersPagination(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateUser(ctx, "u2", CreateUserParams{DisplayName: "B", Email: "b@x.com"})
	s.CreateUser(ctx, "u3", CreateUserParams{DisplayName: "C", Email: "c@x.com"})

	page1, err := s.ListUsers(ctx, ListUsersParams{PageSize: 2})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(page1) < 2 {
		t.Fatalf("got %d users, want at least 2", len(page1))
	}

	page2, err := s.ListUsers(ctx, ListUsersParams{PageSize: 2, PageToken: page1[1].ID})
	if err != nil {
		t.Fatalf("list page 2: %v", err)
	}
	if len(page2) < 1 {
		t.Fatalf("got %d users on page 2, want at least 1", len(page2))
	}
}

// --- Credential ---

func TestCredentialCreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	c, err := s.CreateCredential(ctx, "c1", CreateCredentialParams{
		UserID:         "u1",
		CredentialType: "bearer",
		ExternalID:     "ext-1",
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if c.CredentialType != "bearer" {
		t.Errorf("type = %q", c.CredentialType)
	}
	if c.Metadata != "{}" {
		t.Errorf("metadata = %q, want {}", c.Metadata)
	}
}

func TestGetCredentialByExternal(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateCredential(ctx, "c1", CreateCredentialParams{
		UserID:         "u1",
		CredentialType: "github",
		ExternalID:     "gh-123",
	})

	c, err := s.GetCredentialByExternal(ctx, "github", "gh-123")
	if err != nil {
		t.Fatalf("get by external: %v", err)
	}
	if c.ID != "c1" {
		t.Errorf("id = %q, want c1", c.ID)
	}

	missing, _ := s.GetCredentialByExternal(ctx, "github", "nope")
	if missing != nil {
		t.Error("expected nil for missing external id")
	}
}

func TestListCredentials(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateCredential(ctx, "c1", CreateCredentialParams{UserID: "u1", CredentialType: "bearer"})
	s.CreateCredential(ctx, "c2", CreateCredentialParams{UserID: "u1", CredentialType: "github", ExternalID: "gh"})

	creds, err := s.ListCredentials(ctx, "u1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(creds) != 2 {
		t.Fatalf("got %d, want 2", len(creds))
	}
}

func TestDeleteCredential(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateCredential(ctx, "c1", CreateCredentialParams{UserID: "u1", CredentialType: "bearer"})

	if err := s.DeleteCredential(ctx, "c1"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	c, _ := s.GetCredential(ctx, "c1")
	if c != nil {
		t.Error("credential should be deleted")
	}

	err := s.DeleteCredential(ctx, "c1")
	if err == nil {
		t.Error("second delete should error")
	}
}

// --- Session ---

func TestSessionCreateAndGetByTokenHash(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	sess, err := s.CreateSession(ctx, "sess1", CreateSessionParams{
		UserID:       "u1",
		RefreshToken: "secret-token",
		Provider:     "local",
		IPAddress:    "127.0.0.1",
		UserAgent:    "test",
		ExpiresAt:    expires,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if sess.TokenHash != HashToken("secret-token") {
		t.Error("token_hash mismatch")
	}

	got, err := s.GetSessionByTokenHash(ctx, HashToken("secret-token"))
	if err != nil {
		t.Fatalf("get by hash: %v", err)
	}
	if got == nil {
		t.Fatal("session not found by token hash")
	}
	if got.UserID != "u1" {
		t.Errorf("userID = %q", got.UserID)
	}
}

func TestGetSessionByTokenHashExpired(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	expired := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)

	s.CreateSession(ctx, "sess1", CreateSessionParams{
		UserID:       "u1",
		RefreshToken: "tok",
		ExpiresAt:    expired,
	})

	got, _ := s.GetSessionByTokenHash(ctx, HashToken("tok"))
	if got != nil {
		t.Error("expired session should not be returned")
	}
}

func TestGetSessionByTokenHashRevoked(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	s.CreateSession(ctx, "sess1", CreateSessionParams{
		UserID:       "u1",
		RefreshToken: "tok",
		ExpiresAt:    expires,
	})
	s.RevokeSession(ctx, "sess1")

	got, _ := s.GetSessionByTokenHash(ctx, HashToken("tok"))
	if got != nil {
		t.Error("revoked session should not be returned")
	}
}

func TestRevokeAllSessions(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	s.CreateSession(ctx, "s1", CreateSessionParams{UserID: "u1", RefreshToken: "t1", ExpiresAt: expires})
	s.CreateSession(ctx, "s2", CreateSessionParams{UserID: "u1", RefreshToken: "t2", ExpiresAt: expires})

	if err := s.RevokeAllSessions(ctx, "u1"); err != nil {
		t.Fatalf("revoke all: %v", err)
	}

	got1, _ := s.GetSessionByTokenHash(ctx, HashToken("t1"))
	got2, _ := s.GetSessionByTokenHash(ctx, HashToken("t2"))
	if got1 != nil || got2 != nil {
		t.Error("all sessions should be revoked")
	}
}

func TestCleanExpiredSessions(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	expired := time.Now().Add(-time.Hour).UTC().Format(time.RFC3339)
	active := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)

	s.CreateSession(ctx, "exp", CreateSessionParams{UserID: "u1", RefreshToken: "t1", ExpiresAt: expired})
	s.CreateSession(ctx, "act", CreateSessionParams{UserID: "u1", RefreshToken: "t2", ExpiresAt: active})

	n, err := s.CleanExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("clean: %v", err)
	}
	if n != 1 {
		t.Errorf("cleaned %d, want 1", n)
	}
}

func TestHashToken(t *testing.T) {
	t.Parallel()
	h1 := HashToken("abc")
	h2 := HashToken("abc")
	h3 := HashToken("def")
	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
	if len(h1) != 64 {
		t.Errorf("hash length = %d, want 64 (SHA256 hex)", len(h1))
	}
}

// --- DeviceCode ---

func TestDeviceCodeCreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	err := s.CreateDeviceCode(ctx, CreateDeviceCodeParams{
		DeviceCode: "dc1",
		UserCode:   "ABCD-EFGH",
		ClientID:   "cli",
		Scope:      "openid",
		ExpiresAt:  time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		Interval:   5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	dc, err := s.GetDeviceCode(ctx, "dc1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if dc.State != DeviceCodePending {
		t.Errorf("state = %q, want PENDING", dc.State)
	}
}

func TestGetDeviceCodeByUserCode(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateDeviceCode(ctx, CreateDeviceCodeParams{
		DeviceCode: "dc1",
		UserCode:   "ABCD-EFGH",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		Interval:   5,
	})

	dc, err := s.GetDeviceCodeByUserCode(ctx, "ABCD-EFGH")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if dc.DeviceCode != "dc1" {
		t.Errorf("deviceCode = %q", dc.DeviceCode)
	}
}

func TestApproveDeviceCode(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateDeviceCode(ctx, CreateDeviceCodeParams{
		DeviceCode: "dc1",
		UserCode:   "CODE1",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		Interval:   5,
	})

	if err := s.ApproveDeviceCode(ctx, "dc1", "u1"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	dc, _ := s.GetDeviceCode(ctx, "dc1")
	if dc.State != DeviceCodeApproved {
		t.Errorf("state = %q, want APPROVED", dc.State)
	}
	if dc.UserID == nil || *dc.UserID != "u1" {
		t.Errorf("userID = %v", dc.UserID)
	}

	// Approve again should fail (no longer pending)
	err := s.ApproveDeviceCode(ctx, "dc1", "u1")
	if err == nil {
		t.Error("second approve should fail")
	}
}

func TestDenyDeviceCode(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateDeviceCode(ctx, CreateDeviceCodeParams{
		DeviceCode: "dc1",
		UserCode:   "CODE1",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(10 * time.Minute).UTC().Format(time.RFC3339),
		Interval:   5,
	})

	if err := s.DenyDeviceCode(ctx, "dc1"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	dc, _ := s.GetDeviceCode(ctx, "dc1")
	if dc.State != DeviceCodeDenied {
		t.Errorf("state = %q, want DENIED", dc.State)
	}
}

// --- CIBA ---

func TestCIBACreateAndGet(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	err := s.CreateCIBARequest(ctx, CreateCIBARequestParams{
		AuthReqID:  "ciba1",
		UserID:     "u1",
		ClientID:   "cli",
		Scope:      "openid",
		BindingMsg: "confirm login",
		ExpiresAt:  time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		Interval:   5,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cr, err := s.GetCIBARequest(ctx, "ciba1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if cr.State != CIBAPending {
		t.Errorf("state = %q, want PENDING", cr.State)
	}
	if cr.BindingMsg != "confirm login" {
		t.Errorf("bindingMsg = %q", cr.BindingMsg)
	}
}

func TestApproveCIBARequest(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateCIBARequest(ctx, CreateCIBARequestParams{
		AuthReqID: "ciba1",
		UserID:    "u1",
		ClientID:  "cli",
		ExpiresAt: time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		Interval:  5,
	})

	if err := s.ApproveCIBARequest(ctx, "ciba1"); err != nil {
		t.Fatalf("approve: %v", err)
	}
	cr, _ := s.GetCIBARequest(ctx, "ciba1")
	if cr.State != CIBAApproved {
		t.Errorf("state = %q, want APPROVED", cr.State)
	}

	// Second approve should fail
	err := s.ApproveCIBARequest(ctx, "ciba1")
	if err == nil {
		t.Error("second approve should fail")
	}
}

func TestDenyCIBARequest(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateUser(ctx, "u1", CreateUserParams{DisplayName: "A", Email: "a@x.com"})
	s.CreateCIBARequest(ctx, CreateCIBARequestParams{
		AuthReqID: "ciba1",
		UserID:    "u1",
		ClientID:  "cli",
		ExpiresAt: time.Now().Add(5 * time.Minute).UTC().Format(time.RFC3339),
		Interval:  5,
	})

	if err := s.DenyCIBARequest(ctx, "ciba1"); err != nil {
		t.Fatalf("deny: %v", err)
	}
	cr, _ := s.GetCIBARequest(ctx, "ciba1")
	if cr.State != CIBADenied {
		t.Errorf("state = %q, want DENIED", cr.State)
	}
}

// --- Gap-filling tests ---

func TestGetRunNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	run, err := s.GetRun(context.Background(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if run != nil {
		t.Fatalf("expected nil, got %+v", run)
	}
}

func TestGetScheduleNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	sc, err := s.GetSchedule(context.Background(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sc != nil {
		t.Fatalf("expected nil, got %+v", sc)
	}
}

func TestGetUserNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	u, err := s.GetUser(context.Background(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if u != nil {
		t.Fatalf("expected nil, got %+v", u)
	}
}

func TestGetCredentialNotFound(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	c, err := s.GetCredential(context.Background(), "nope")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c != nil {
		t.Fatalf("expected nil, got %+v", c)
	}
}

func TestListTasksPageToken(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := "t" + string(rune('a'+i))
		s.CreateTask(ctx, id, CreateTaskParams{Prompt: "p", RepoURL: "r"})
	}

	// Get first page
	page1, err := s.ListTasks(ctx, ListTasksParams{PageSize: 2})
	if err != nil {
		t.Fatalf("list page1: %v", err)
	}
	if len(page1) < 2 {
		t.Fatalf("got %d tasks on page1, want at least 2", len(page1))
	}

	// Use the last item's ID as page token for continuation
	page2, err := s.ListTasks(ctx, ListTasksParams{PageSize: 2, PageToken: page1[1].ID})
	if err != nil {
		t.Fatalf("list page2: %v", err)
	}
	if len(page2) < 1 {
		t.Fatalf("got %d tasks on page2, want at least 1", len(page2))
	}
	// Ensure no overlap
	if page2[0].ID == page1[0].ID || page2[0].ID == page1[1].ID {
		t.Errorf("page2 overlaps with page1: %q", page2[0].ID)
	}
}

func TestListRunsEmpty(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	s.CreateTask(ctx, "t1", CreateTaskParams{Prompt: "p", RepoURL: "r"})
	runs, err := s.ListRuns(ctx, "t1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("got %d runs, want 0", len(runs))
	}
}

func TestAdvanceScheduleNilNextRun(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	ctx := context.Background()

	next := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	s.CreateSchedule(ctx, "s1", CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: next,
	})

	now := time.Now().UTC()
	// Pass nil nextRun to simulate a terminal schedule
	if err := s.AdvanceSchedule(ctx, "s1", now, nil); err != nil {
		t.Fatalf("advance: %v", err)
	}

	sc, _ := s.GetSchedule(ctx, "s1")
	if sc.RunCount != 1 {
		t.Errorf("run_count = %d, want 1", sc.RunCount)
	}
	if sc.NextRunTime != nil {
		t.Errorf("next_run_time = %v, want nil", sc.NextRunTime)
	}
}
