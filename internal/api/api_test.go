package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/SCKelemen/orchestrate/internal/store"
)

// helper to create an authenticated request (auth middleware is permissive in test)
func authedRequest(method, path string, body any) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-token")
	return req
}

func decodeJSON(t *testing.T, rr *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.NewDecoder(rr.Body).Decode(v); err != nil {
		t.Fatalf("decode json: %v (body: %s)", err, rr.Body.String())
	}
}

// --- Health ---

func TestHealthz(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	if rr.Body.String() != "ok" {
		t.Errorf("body = %q, want ok", rr.Body.String())
	}
}

// --- Tasks ---

func TestCreateTaskValid(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createTaskRequest{
		Prompt:  "implement feature X",
		RepoURL: "https://github.com/test/repo",
		Title:   "Feature X",
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks", body))

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}
	var resp taskResponse
	decodeJSON(t, rr, &resp)
	if !strings.HasPrefix(resp.Name, "tasks/") {
		t.Errorf("name = %q, want tasks/ prefix", resp.Name)
	}
	if resp.State != "QUEUED" {
		t.Errorf("state = %q, want QUEUED", resp.State)
	}
}

func TestCreateTaskMissingPrompt(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createTaskRequest{RepoURL: "https://github.com/test/repo"}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks", body))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "prompt is required") {
		t.Errorf("body = %s", rr.Body.String())
	}
}

func TestCreateTaskMissingRepoUrl(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createTaskRequest{Prompt: "do something"}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks", body))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "repoUrl is required") {
		t.Errorf("body = %s", rr.Body.String())
	}
}

func TestListTasksEmpty(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var resp map[string]json.RawMessage
	decodeJSON(t, rr, &resp)
	var tasks []json.RawMessage
	json.Unmarshal(resp["tasks"], &tasks)
	if len(tasks) != 0 {
		t.Errorf("got %d tasks, want 0", len(tasks))
	}
}

func TestListTasksWithItems(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.CreateTask(ctx, "t2", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp map[string]json.RawMessage
	decodeJSON(t, rr, &resp)
	var tasks []json.RawMessage
	json.Unmarshal(resp["tasks"], &tasks)
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestListTasksPagination(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		id := string(rune('a'+i)) + "task"
		st.CreateTask(ctx, id, store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	}

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks?pageSize=2", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp map[string]json.RawMessage
	decodeJSON(t, rr, &resp)
	var tasks []json.RawMessage
	json.Unmarshal(resp["tasks"], &tasks)
	if len(tasks) != 2 {
		t.Errorf("got %d tasks, want 2", len(tasks))
	}
}

func TestGetTaskFound(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{
		Prompt:  "do it",
		RepoURL: "r",
		Title:   "Test",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp taskResponse
	decodeJSON(t, rr, &resp)
	if resp.Title != "Test" {
		t.Errorf("title = %q", resp.Title)
	}
}

func TestGetTaskNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/nope", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestUpdateTask(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	title := "updated"
	body := updateTaskRequest{Title: &title}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPatch, "/v1/tasks/t1", body))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp taskResponse
	decodeJSON(t, rr, &resp)
	if resp.Title != "updated" {
		t.Errorf("title = %q", resp.Title)
	}
}

func TestDeleteTask(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodDelete, "/v1/tasks/t1", nil))

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
}

func TestCancelTaskValid(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks/t1/:cancel", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp taskResponse
	decodeJSON(t, rr, &resp)
	if resp.State != "CANCELLED" {
		t.Errorf("state = %q, want CANCELLED", resp.State)
	}
}

func TestCancelTaskWrongState(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.UpdateTaskState(ctx, "t1", store.TaskSucceeded)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks/t1/:cancel", nil))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestRetryTask(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.UpdateTaskState(ctx, "t1", store.TaskFailed)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks/t1/:retry", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp taskResponse
	decodeJSON(t, rr, &resp)
	if resp.State != "QUEUED" {
		t.Errorf("state = %q, want QUEUED", resp.State)
	}
}

// --- Schedules ---

func TestCreateScheduleValid(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createScheduleRequest{
		ScheduleExpr: "0 0 * * *",
		Prompt:       "nightly cleanup",
		RepoURL:      "https://github.com/test/repo",
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules", body))

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp scheduleResponse
	decodeJSON(t, rr, &resp)
	if !strings.HasPrefix(resp.Name, "schedules/") {
		t.Errorf("name = %q", resp.Name)
	}
	if resp.State != "ACTIVE" {
		t.Errorf("state = %q, want ACTIVE", resp.State)
	}
}

func TestCreateScheduleInvalidExpr(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createScheduleRequest{
		ScheduleExpr: "not-a-schedule",
		Prompt:       "p",
		RepoURL:      "r",
	}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules", body))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
}

func TestListSchedules(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateSchedule(ctx, "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/schedules", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp map[string]json.RawMessage
	decodeJSON(t, rr, &resp)
	var schedules []json.RawMessage
	json.Unmarshal(resp["schedules"], &schedules)
	if len(schedules) != 1 {
		t.Errorf("got %d schedules, want 1", len(schedules))
	}
}

func TestGetScheduleFound(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateSchedule(context.Background(), "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
		Title: "Hourly",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/schedules/s1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp scheduleResponse
	decodeJSON(t, rr, &resp)
	if resp.Title != "Hourly" {
		t.Errorf("title = %q", resp.Title)
	}
}

func TestUpdateSchedule(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateSchedule(context.Background(), "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})

	title := "Updated"
	body := updateScheduleRequest{Title: &title}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPatch, "/v1/schedules/s1", body))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp scheduleResponse
	decodeJSON(t, rr, &resp)
	if resp.Title != "Updated" {
		t.Errorf("title = %q", resp.Title)
	}
}

func TestDeleteSchedule(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateSchedule(context.Background(), "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodDelete, "/v1/schedules/s1", nil))

	if rr.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rr.Code)
	}
}

func TestPauseSchedule(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateSchedule(context.Background(), "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules/s1/:pause", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp scheduleResponse
	decodeJSON(t, rr, &resp)
	if resp.State != "PAUSED" {
		t.Errorf("state = %q, want PAUSED", resp.State)
	}
}

func TestResumeSchedule(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateSchedule(ctx, "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})
	st.UpdateScheduleState(ctx, "s1", store.SchedulePaused)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules/s1/:resume", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp scheduleResponse
	decodeJSON(t, rr, &resp)
	if resp.State != "ACTIVE" {
		t.Errorf("state = %q, want ACTIVE", resp.State)
	}
}

// --- Runs ---

func TestListRuns(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.CreateRun(ctx, "r1", store.CreateRunParams{TaskID: "t1", AgentIndex: 0, Branch: "b0"})
	st.CreateRun(ctx, "r2", store.CreateRunParams{TaskID: "t1", AgentIndex: 1, Branch: "b1"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1/runs", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d", rr.Code)
	}
	var resp map[string]json.RawMessage
	decodeJSON(t, rr, &resp)
	var runs []json.RawMessage
	json.Unmarshal(resp["runs"], &runs)
	if len(runs) != 2 {
		t.Errorf("got %d runs, want 2", len(runs))
	}
}

func TestGetRun(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.CreateRun(ctx, "r1", store.CreateRunParams{TaskID: "t1", AgentIndex: 0, Branch: "b0"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1/runs/r1", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	var resp runResponse
	decodeJSON(t, rr, &resp)
	if resp.Branch != "b0" {
		t.Errorf("branch = %q", resp.Branch)
	}
}

func TestGetRunNotFound(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1/runs/nope", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestStreamLogs(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	// Create a temp log file
	logDir := t.TempDir()
	logFile := filepath.Join(logDir, "run.log")
	os.WriteFile(logFile, []byte("line1\nline2\nline3\n"), 0o644)

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.CreateRun(ctx, "r1", store.CreateRunParams{
		TaskID:     "t1",
		AgentIndex: 0,
		Branch:     "b0",
		LogPath:    logFile,
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1/runs/r1/:logs", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d; body=%s", rr.Code, rr.Body.String())
	}
	body := rr.Body.String()
	if !strings.Contains(body, "data: line1") {
		t.Errorf("missing line1 in SSE output: %s", body)
	}
	if !strings.Contains(body, "data: line2") {
		t.Errorf("missing line2 in SSE output: %s", body)
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("missing done event: %s", body)
	}
}

func TestStreamLogsNoLogFile(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.CreateRun(ctx, "r1", store.CreateRunParams{
		TaskID:     "t1",
		AgentIndex: 0,
		Branch:     "b0",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1/runs/r1/:logs", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

// --- Gap-filling tests ---

func TestUnknownTaskCustomMethod(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks/t1/:unknown", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestUnknownRunAction(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()
	st.CreateTask(ctx, "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})
	st.CreateRun(ctx, "r1", store.CreateRunParams{TaskID: "t1", AgentIndex: 0, Branch: "b0"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/tasks/t1/runs/r1/:unknown", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestUnknownScheduleAction(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	st.CreateSchedule(context.Background(), "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules/s1/:unknown", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestCreateScheduleMissingPrompt(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createScheduleRequest{ScheduleExpr: "0 * * * *", RepoURL: "r"}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestCreateScheduleMissingRepoUrl(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := createScheduleRequest{ScheduleExpr: "0 * * * *", Prompt: "p"}
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestGetScheduleNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodGet, "/v1/schedules/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestDeleteScheduleNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodDelete, "/v1/schedules/nope", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestPauseAlreadyPausedSchedule(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	st.CreateSchedule(ctx, "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})
	st.UpdateScheduleState(ctx, "s1", store.SchedulePaused)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules/s1/:pause", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestResumeAlreadyActiveSchedule(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	st.CreateSchedule(context.Background(), "s1", store.CreateScheduleParams{
		ScheduleExpr: "0 * * * *", ScheduleType: "CRON",
		Prompt: "p", RepoURL: "r", NextRunTime: "2099-01-01T00:00:00Z",
	})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/schedules/s1/:resume", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestRetryTaskWrongState(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)

	// Task in QUEUED state cannot be retried (only FAILED/CANCELLED can)
	st.CreateTask(context.Background(), "t1", store.CreateTaskParams{Prompt: "p", RepoURL: "r"})

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks/t1/:retry", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
}

func TestCancelTaskNotFound(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, authedRequest(http.MethodPost, "/v1/tasks/nope/:cancel", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rr.Code)
	}
}

func TestUnsupportedGrantType(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := map[string]string{"grant_type": "magic"}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", nil)
	req.Header.Set("Content-Type", "application/json")
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req = httptest.NewRequest(http.MethodPost, "/v1/auth/token", &buf)
	req.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unsupported grant_type") {
		t.Errorf("body = %s", rr.Body.String())
	}
}

func TestRevokeInvalidToken(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := map[string]string{"refresh_token": "invalid-token-that-does-not-exist"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/token/:revoke", &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	// Per RFC 7009, revoking an invalid token is not an error
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (RFC 7009)", rr.Code)
	}
}

func TestRefreshTokenMissing(t *testing.T) {
	t.Parallel()
	srv, _ := newTestServer(t)

	body := map[string]string{"grant_type": "refresh_token"}
	var buf bytes.Buffer
	json.NewEncoder(&buf).Encode(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/token", &buf)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "refresh_token is required") {
		t.Errorf("body = %s", rr.Body.String())
	}
}
