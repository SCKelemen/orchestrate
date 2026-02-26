package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "api.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	mw := auth.NewMiddleware()
	signer := auth.NewSigner([]byte("test-secret"), "orchestrate-test")
	srv := NewServer(st, mw, signer, logger)
	return srv, st
}

func TestAuthorizePageEscapesHiddenInputValues(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {"cli\"><script>alert(1)</script>"},
		"redirect_uri":          {"http://localhost/callback"},
		"code_challenge":        {"abc123"},
		"code_challenge_method": {"S256"},
		"state":                 {"x\"><img src=x onerror=alert(1)>"},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/authorize?"+q.Encode(), nil)
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "<script>alert(1)</script>") {
		t.Fatalf("response contains unescaped script tag: %q", body)
	}
	if strings.Contains(body, "<img src=x onerror=alert(1)>") {
		t.Fatalf("response contains unescaped HTML tag injection: %q", body)
	}
}

func TestAuthorizeSubmitRequiresCodeChallenge(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)

	form := url.Values{
		"email":                 {"user@example.com"},
		"display_name":          {"User"},
		"client_id":             {"cli"},
		"redirect_uri":          {"http://localhost/callback"},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/authorize", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "code_challenge is required") {
		t.Fatalf("unexpected error body: %s", rr.Body.String())
	}
}

func TestDeviceVerifyPageEscapesUserCode(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/device/verify?user_code="+url.QueryEscape(`x"><script>alert(1)</script>`), nil)
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, `<script>alert(1)</script>`) {
		t.Fatalf("response contains unescaped script tag: %q", body)
	}
}

func TestDeviceVerifySubmitRejectsInvalidAction(t *testing.T) {
	t.Parallel()

	srv, _ := newTestServer(t)
	form := url.Values{
		"user_code": {"ABCD-EFGH"},
		"action":    {"maybe"},
		"email":     {"u@example.com"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/device/verify", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "action must be approve or deny") {
		t.Fatalf("unexpected error body: %s", rr.Body.String())
	}
}

func TestAuthCodeGrantCanOnlyBeUsedOnce(t *testing.T) {
	t.Parallel()

	srv, st := newTestServer(t)
	ctx := context.Background()

	user, err := st.CreateUser(ctx, "u1", store.CreateUserParams{
		DisplayName: "User",
		Email:       "u1@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	verifier := "test-code-verifier"
	challenge := auth.CodeChallengeS256(verifier)
	code := "auth-code-1"
	if err := st.CreateAuthCode(ctx, code, store.CreateAuthCodeParams{
		UserID:              user.ID,
		ClientID:            "cli",
		RedirectURI:         "http://localhost/callback",
		Scope:               "openid",
		CodeChallenge:       challenge,
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("create auth code: %v", err)
	}

	bodyReq := tokenRequest{
		GrantType:    "authorization_code",
		Code:         code,
		ClientID:     "cli",
		RedirectURI:  "http://localhost/callback",
		CodeVerifier: verifier,
	}
	bodyJSON, err := json.Marshal(bodyReq)
	if err != nil {
		t.Fatalf("marshal token request: %v", err)
	}

	first := httptest.NewRecorder()
	req1 := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(bodyJSON))
	req1.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(first, req1)
	if first.Code != http.StatusOK {
		t.Fatalf("first exchange status = %d, want 200; body=%s", first.Code, first.Body.String())
	}

	second := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "/v1/auth/token", bytes.NewReader(bodyJSON))
	req2.Header.Set("Content-Type", "application/json")
	srv.ServeHTTP(second, req2)
	if second.Code != http.StatusBadRequest {
		t.Fatalf("second exchange status = %d, want 400; body=%s", second.Code, second.Body.String())
	}
	if !strings.Contains(second.Body.String(), "authorization code already used") {
		t.Fatalf("unexpected second exchange body: %s", second.Body.String())
	}
}
