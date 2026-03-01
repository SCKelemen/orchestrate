package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCreateTaskRejectsTooManyAgents(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"prompt":"do work","repoUrl":"https://example.com/repo.git","agentCount":999}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agentCount") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateTaskRejectsUnsupportedStrategy(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"prompt":"do work","repoUrl":"https://example.com/repo.git","strategy":"PWN"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "unsupported strategy") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateTaskRejectsOversizedPrompt(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	oversized := strings.Repeat("a", maxPromptSize+1)
	body := `{"prompt":"` + oversized + `","repoUrl":"https://example.com/repo.git"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "prompt exceeds") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateScheduleRejectsTooManyAgents(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{
		"scheduleExpr":"0 * * * *",
		"prompt":"do scheduled work",
		"repoUrl":"https://example.com/repo.git",
		"agentCount":999
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "agentCount") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}
