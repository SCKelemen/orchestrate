package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/store"
)

func mustIssueToken(t *testing.T, signer *auth.Signer, userID string) string {
	t.Helper()
	token, err := signer.IssueAccessToken(&auth.Identity{
		UserID:   userID,
		Provider: "github",
	}, "jti-"+userID)
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	return token
}

func createTaskViaAPI(t *testing.T, srv *Server, token string, prompt string) string {
	t.Helper()
	body := map[string]any{
		"prompt":  prompt,
		"repoUrl": "https://example.com/repo.git",
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create task status=%d body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode create task response: %v", err)
	}
	return strings.TrimPrefix(out.Name, "tasks/")
}

func createScheduleViaAPI(t *testing.T, srv *Server, token string, expr string) string {
	t.Helper()
	body := map[string]any{
		"scheduleExpr": expr,
		"prompt":       "scheduled prompt",
		"repoUrl":      "https://example.com/repo.git",
	}
	data, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create schedule status=%d body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode create schedule response: %v", err)
	}
	return strings.TrimPrefix(out.Name, "schedules/")
}

func TestTaskAuthorizationIsolation(t *testing.T) {
	t.Parallel()

	srv, st, signer, adminToken := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{Email: "u1@example.com"}); err != nil {
		t.Fatalf("create user u1: %v", err)
	}
	if _, err := st.CreateUser(ctx, "u2", store.CreateUserParams{Email: "u2@example.com"}); err != nil {
		t.Fatalf("create user u2: %v", err)
	}

	u1Token := mustIssueToken(t, signer, "u1")
	u2Token := mustIssueToken(t, signer, "u2")
	taskID := createTaskViaAPI(t, srv, u1Token, "u1 task")

	reqOther := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID, nil)
	reqOther.Header.Set("Authorization", "Bearer "+u2Token)
	rrOther := httptest.NewRecorder()
	srv.ServeHTTP(rrOther, reqOther)
	if rrOther.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=403 body=%s", rrOther.Code, rrOther.Body.String())
	}

	reqOwner := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID, nil)
	reqOwner.Header.Set("Authorization", "Bearer "+u1Token)
	rrOwner := httptest.NewRecorder()
	srv.ServeHTTP(rrOwner, reqOwner)
	if rrOwner.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rrOwner.Code, rrOwner.Body.String())
	}

	reqAdmin := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID, nil)
	reqAdmin.Header.Set("Authorization", "Bearer "+adminToken)
	rrAdmin := httptest.NewRecorder()
	srv.ServeHTTP(rrAdmin, reqAdmin)
	if rrAdmin.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rrAdmin.Code, rrAdmin.Body.String())
	}
}

func TestTaskListIsOwnerScoped(t *testing.T) {
	t.Parallel()

	srv, st, signer, _ := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{Email: "u1@example.com"}); err != nil {
		t.Fatalf("create user u1: %v", err)
	}
	if _, err := st.CreateUser(ctx, "u2", store.CreateUserParams{Email: "u2@example.com"}); err != nil {
		t.Fatalf("create user u2: %v", err)
	}

	u1Token := mustIssueToken(t, signer, "u1")
	u2Token := mustIssueToken(t, signer, "u2")
	_ = createTaskViaAPI(t, srv, u1Token, "u1 task")
	_ = createTaskViaAPI(t, srv, u2Token, "u2 task")

	req := httptest.NewRequest(http.MethodGet, "/v1/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+u1Token)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Tasks []struct {
			Prompt string `json:"prompt"`
		} `json:"tasks"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(out.Tasks) != 1 {
		t.Fatalf("tasks len=%d want=1 body=%s", len(out.Tasks), rr.Body.String())
	}
	if out.Tasks[0].Prompt != "u1 task" {
		t.Fatalf("unexpected task prompt=%q", out.Tasks[0].Prompt)
	}
}

func TestScheduleAuthorizationIsolation(t *testing.T) {
	t.Parallel()

	srv, st, signer, _ := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{Email: "u1@example.com"}); err != nil {
		t.Fatalf("create user u1: %v", err)
	}
	if _, err := st.CreateUser(ctx, "u2", store.CreateUserParams{Email: "u2@example.com"}); err != nil {
		t.Fatalf("create user u2: %v", err)
	}

	u1Token := mustIssueToken(t, signer, "u1")
	u2Token := mustIssueToken(t, signer, "u2")
	scheduleID := createScheduleViaAPI(t, srv, u1Token, "0 * * * *")

	reqOther := httptest.NewRequest(http.MethodGet, "/v1/schedules/"+scheduleID, nil)
	reqOther.Header.Set("Authorization", "Bearer "+u2Token)
	rrOther := httptest.NewRecorder()
	srv.ServeHTTP(rrOther, reqOther)
	if rrOther.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=403 body=%s", rrOther.Code, rrOther.Body.String())
	}

	reqOwner := httptest.NewRequest(http.MethodGet, "/v1/schedules/"+scheduleID, nil)
	reqOwner.Header.Set("Authorization", "Bearer "+u1Token)
	rrOwner := httptest.NewRecorder()
	srv.ServeHTTP(rrOwner, reqOwner)
	if rrOwner.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rrOwner.Code, rrOwner.Body.String())
	}
}

func TestRunAuthorizationIsolation(t *testing.T) {
	t.Parallel()

	srv, st, signer, _ := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{Email: "u1@example.com"}); err != nil {
		t.Fatalf("create user u1: %v", err)
	}
	if _, err := st.CreateUser(ctx, "u2", store.CreateUserParams{Email: "u2@example.com"}); err != nil {
		t.Fatalf("create user u2: %v", err)
	}

	task, err := st.CreateTask(ctx, "t1", store.CreateTaskParams{
		OwnerUserID: "u1",
		Prompt:      "task prompt",
		RepoURL:     "https://example.com/repo.git",
	})
	if err != nil {
		t.Fatalf("create task: %v", err)
	}
	if _, err := st.CreateRun(ctx, "r1", store.CreateRunParams{
		TaskID:     task.ID,
		AgentIndex: 0,
		Branch:     "main",
		LogPath:    "",
	}); err != nil {
		t.Fatalf("create run: %v", err)
	}
	if err := st.UpdateRunState(ctx, "r1", store.RunSucceeded, intPtr(0), "ok"); err != nil {
		t.Fatalf("update run: %v", err)
	}

	u2Token := mustIssueToken(t, signer, "u2")
	reqOther := httptest.NewRequest(http.MethodGet, "/v1/tasks/t1/runs/r1", nil)
	reqOther.Header.Set("Authorization", "Bearer "+u2Token)
	rrOther := httptest.NewRecorder()
	srv.ServeHTTP(rrOther, reqOther)
	if rrOther.Code != http.StatusForbidden {
		t.Fatalf("status=%d want=403 body=%s", rrOther.Code, rrOther.Body.String())
	}

	u1Token := mustIssueToken(t, signer, "u1")
	reqOwner := httptest.NewRequest(http.MethodGet, "/v1/tasks/t1/runs/r1", nil)
	reqOwner.Header.Set("Authorization", "Bearer "+u1Token)
	rrOwner := httptest.NewRecorder()
	srv.ServeHTTP(rrOwner, reqOwner)
	if rrOwner.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rrOwner.Code, rrOwner.Body.String())
	}
}

func intPtr(v int) *int { return &v }

func TestCIBAApproveAllowsAdminOverride(t *testing.T) {
	t.Parallel()

	srv, st, _, adminToken := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{Email: "u1@example.com"}); err != nil {
		t.Fatalf("create user u1: %v", err)
	}
	if err := st.CreateCIBARequest(ctx, store.CreateCIBARequestParams{
		AuthReqID:  "req-admin",
		UserID:     "u1",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
		WebhookURL: "",
	}); err != nil {
		t.Fatalf("create ciba request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba/:approve", strings.NewReader(`{"auth_req_id":"req-admin"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d want=200 body=%s", rr.Code, rr.Body.String())
	}
}
