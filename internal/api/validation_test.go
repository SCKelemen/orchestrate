package api

import (
	"encoding/json"
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

func TestCreateTaskAcceptsAdversarialStrategy(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"prompt":"do work","repoUrl":"https://example.com/repo.git","strategy":"ADVERSARIAL"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Strategy string `json:"strategy"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Strategy != "ADVERSARIAL" {
		t.Fatalf("strategy=%q want=ADVERSARIAL", out.Strategy)
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

func TestCreateScheduleAcceptsCodeAndTestStrategy(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{
		"scheduleExpr":"0 * * * *",
		"prompt":"do scheduled work",
		"repoUrl":"https://example.com/repo.git",
		"strategy":"CODE_AND_TEST"
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Strategy string `json:"strategy"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Strategy != "CODE_AND_TEST" {
		t.Fatalf("strategy=%q want=CODE_AND_TEST", out.Strategy)
	}
}

func TestCreateTaskRejectsUnsupportedAgentBackend(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"agent":"unknown","prompt":"do work","repoUrl":"https://example.com/repo.git"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "unsupported agent backend") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateTaskAcceptsOpenAIAliasAsCodex(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"agent":"openai","prompt":"do work","repoUrl":"https://example.com/repo.git"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Agent != "codex" {
		t.Fatalf("agent=%q want=codex", out.Agent)
	}
}

func TestCreateTaskRejectsDisallowedImage(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"prompt":"do work","repoUrl":"https://example.com/repo.git","image":"ghcr.io/acme/custom-agent:latest"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "image is not allowed") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateTaskAllowsConfiguredImage(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServerWithOptions(
		t,
		WithImagePolicy([]string{"ghcr.io/acme/custom-agent:latest"}, false),
	)
	body := `{"prompt":"do work","repoUrl":"https://example.com/repo.git","image":"ghcr.io/acme/custom-agent:latest"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Image string `json:"image"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Image != "ghcr.io/acme/custom-agent:latest" {
		t.Fatalf("image=%q want=ghcr.io/acme/custom-agent:latest", out.Image)
	}
}

func TestCreateScheduleRejectsDisallowedImage(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{
		"scheduleExpr":"0 * * * *",
		"prompt":"do scheduled work",
		"repoUrl":"https://example.com/repo.git",
		"image":"ghcr.io/acme/custom-agent:latest"
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "image is not allowed") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateTaskDefaultsAgentToClaude(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{"prompt":"do work","repoUrl":"https://example.com/repo.git"}`

	req := httptest.NewRequest(http.MethodPost, "/v1/tasks", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Agent != "claude" {
		t.Fatalf("agent=%q want=claude", out.Agent)
	}
}

func TestCreateScheduleRejectsUnsupportedAgentBackend(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{
		"agent":"unknown",
		"scheduleExpr":"0 * * * *",
		"prompt":"do scheduled work",
		"repoUrl":"https://example.com/repo.git"
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "unsupported agent backend") {
		t.Fatalf("unexpected body: %s", rr.Body.String())
	}
}

func TestCreateScheduleAcceptsOpenAIAliasAsCodex(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{
		"agent":"openai",
		"scheduleExpr":"0 * * * *",
		"prompt":"do scheduled work",
		"repoUrl":"https://example.com/repo.git"
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Agent != "codex" {
		t.Fatalf("agent=%q want=codex", out.Agent)
	}
}

func TestCreateScheduleDefaultsAgentToClaude(t *testing.T) {
	t.Parallel()

	srv, _, _, adminToken := newSecuredTestServer(t)
	body := `{
		"scheduleExpr":"0 * * * *",
		"prompt":"do scheduled work",
		"repoUrl":"https://example.com/repo.git"
	}`

	req := httptest.NewRequest(http.MethodPost, "/v1/schedules", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rr.Code, rr.Body.String())
	}

	var out struct {
		Agent string `json:"agent"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.Agent != "claude" {
		t.Fatalf("agent=%q want=claude", out.Agent)
	}
}
