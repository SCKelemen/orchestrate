package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net"
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
	srv := NewServer(st, mw, signer, logger, WithInsecureEmailAuth(true))
	return srv, st
}

func newSecuredTestServer(t *testing.T) (*Server, *store.Store, *auth.Signer, string) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "api-secure.db")
	st, err := store.Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	signer := auth.NewSigner([]byte("test-secret"), "orchestrate-test")
	adminToken := "admin-secret-token"
	mw := auth.NewMiddleware(
		auth.NewJWTProvider(signer),
		auth.NewBearerProvider(adminToken),
	)

	srv := NewServer(st, mw, signer, logger)
	return srv, st, signer, adminToken
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

func TestCIBAInitiateRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv, st, _, _ := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{
		DisplayName: "User One",
		Email:       "u1@example.com",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	body := bytes.NewBufferString(`{"login_hint":"u1@example.com"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCIBAApproveRequiresAuthentication(t *testing.T) {
	t.Parallel()

	srv, st, _, _ := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{
		DisplayName: "User One",
		Email:       "u1@example.com",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.CreateCIBARequest(ctx, store.CreateCIBARequestParams{
		AuthReqID:  "req-1",
		UserID:     "u1",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
		WebhookURL: "",
	}); err != nil {
		t.Fatalf("create ciba request: %v", err)
	}

	body := bytes.NewBufferString(`{"auth_req_id":"req-1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba/:approve", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rr.Code, rr.Body.String())
	}
}

func TestCIBAApproveEnforcesOwnershipForJWTUsers(t *testing.T) {
	t.Parallel()

	srv, st, signer, _ := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "owner", store.CreateUserParams{
		DisplayName: "Owner",
		Email:       "owner@example.com",
	}); err != nil {
		t.Fatalf("create owner user: %v", err)
	}
	if err := st.CreateCIBARequest(ctx, store.CreateCIBARequestParams{
		AuthReqID:  "req-2",
		UserID:     "owner",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
		WebhookURL: "",
	}); err != nil {
		t.Fatalf("create ciba request: %v", err)
	}

	otherToken, err := signer.IssueAccessToken(&auth.Identity{
		UserID:   "other-user",
		Provider: "github",
	}, "jti-other")
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}

	forbiddenReq := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba/:approve", bytes.NewBufferString(`{"auth_req_id":"req-2"}`))
	forbiddenReq.Header.Set("Content-Type", "application/json")
	forbiddenReq.Header.Set("Authorization", "Bearer "+otherToken)
	forbidden := httptest.NewRecorder()
	srv.ServeHTTP(forbidden, forbiddenReq)

	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", forbidden.Code, forbidden.Body.String())
	}

	ownerToken, err := signer.IssueAccessToken(&auth.Identity{
		UserID:   "owner",
		Provider: "github",
	}, "jti-owner")
	if err != nil {
		t.Fatalf("issue owner token: %v", err)
	}
	okReq := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba/:approve", bytes.NewBufferString(`{"auth_req_id":"req-2"}`))
	okReq.Header.Set("Content-Type", "application/json")
	okReq.Header.Set("Authorization", "Bearer "+ownerToken)
	okResp := httptest.NewRecorder()
	srv.ServeHTTP(okResp, okReq)

	if okResp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", okResp.Code, okResp.Body.String())
	}
}

func TestCIBAInitiateRejectsUnsafeWebhookURL(t *testing.T) {
	t.Parallel()

	srv, st, _, adminToken := newSecuredTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "u1", store.CreateUserParams{
		DisplayName: "User One",
		Email:       "u1@example.com",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}

	body := bytes.NewBufferString(`{"login_hint":"u1@example.com","webhook_url":"https://127.0.0.1/hook"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba", body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+adminToken)
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body=%s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "invalid webhook_url") {
		t.Fatalf("unexpected response body: %s", rr.Body.String())
	}
}

func TestValidateWebhookURL(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		raw       string
		shouldErr bool
	}{
		{name: "valid public https", raw: "https://example.com/hook", shouldErr: false},
		{name: "http rejected", raw: "http://example.com/hook", shouldErr: true},
		{name: "localhost rejected", raw: "https://localhost/hook", shouldErr: true},
		{name: "private ip rejected", raw: "https://10.0.0.1/hook", shouldErr: true},
		{name: "loopback ip rejected", raw: "https://127.0.0.1/hook", shouldErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := validateWebhookURL(tc.raw)
			if tc.shouldErr && err == nil {
				t.Fatalf("expected error for %q", tc.raw)
			}
			if !tc.shouldErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
		})
	}
}

func TestIsDisallowedWebhookIP(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ip   string
		want bool
	}{
		{name: "loopback", ip: "127.0.0.1", want: true},
		{name: "private", ip: "10.1.2.3", want: true},
		{name: "cgnat", ip: "100.64.10.10", want: true},
		{name: "public", ip: "8.8.8.8", want: false},
		{name: "public-v6", ip: "2606:4700:4700::1111", want: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := isDisallowedWebhookIP(net.ParseIP(tc.ip))
			if got != tc.want {
				t.Fatalf("isDisallowedWebhookIP(%s)=%v want=%v", tc.ip, got, tc.want)
			}
		})
	}
}

func TestDeviceCodeGrantCanOnlyBeUsedOnce(t *testing.T) {
	t.Parallel()

	srv, st := newTestServer(t)
	ctx := context.Background()

	user, err := st.CreateUser(ctx, "dev-user", store.CreateUserParams{
		DisplayName: "Device User",
		Email:       "device@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	code := "device-code-1"
	if err := st.CreateDeviceCode(ctx, store.CreateDeviceCodeParams{
		DeviceCode: code,
		UserCode:   "ABCD-EFGH",
		ClientID:   "cli",
		Scope:      "",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
	}); err != nil {
		t.Fatalf("create device code: %v", err)
	}
	if err := st.ApproveDeviceCode(ctx, code, user.ID); err != nil {
		t.Fatalf("approve device code: %v", err)
	}

	body := `{"grant_type":"urn:ietf:params:oauth:grant-type:device_code","device_code":"` + code + `"}`

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/auth/token", strings.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRR := httptest.NewRecorder()
	srv.ServeHTTP(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first exchange status=%d want=200 body=%s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/auth/token", strings.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRR := httptest.NewRecorder()
	srv.ServeHTTP(secondRR, secondReq)
	if secondRR.Code != http.StatusBadRequest {
		t.Fatalf("second exchange status=%d want=400 body=%s", secondRR.Code, secondRR.Body.String())
	}
	if !strings.Contains(secondRR.Body.String(), "invalid_grant") {
		t.Fatalf("unexpected second exchange body: %s", secondRR.Body.String())
	}
}

func TestDevicePollCanOnlyBeUsedOnce(t *testing.T) {
	t.Parallel()

	srv, st := newTestServer(t)
	ctx := context.Background()

	user, err := st.CreateUser(ctx, "dev-user-2", store.CreateUserParams{
		DisplayName: "Device User 2",
		Email:       "device2@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	code := "device-code-2"
	if err := st.CreateDeviceCode(ctx, store.CreateDeviceCodeParams{
		DeviceCode: code,
		UserCode:   "WXYZ-1234",
		ClientID:   "cli",
		Scope:      "",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
	}); err != nil {
		t.Fatalf("create device code: %v", err)
	}
	if err := st.ApproveDeviceCode(ctx, code, user.ID); err != nil {
		t.Fatalf("approve device code: %v", err)
	}

	body := `{"device_code":"` + code + `"}`

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/auth/device/:poll", strings.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRR := httptest.NewRecorder()
	srv.ServeHTTP(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first poll status=%d want=200 body=%s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/auth/device/:poll", strings.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRR := httptest.NewRecorder()
	srv.ServeHTTP(secondRR, secondReq)
	if secondRR.Code != http.StatusBadRequest {
		t.Fatalf("second poll status=%d want=400 body=%s", secondRR.Code, secondRR.Body.String())
	}
	if !strings.Contains(secondRR.Body.String(), "invalid_grant") {
		t.Fatalf("unexpected second poll body: %s", secondRR.Body.String())
	}
}

func TestCIBAGrantCanOnlyBeUsedOnce(t *testing.T) {
	t.Parallel()

	srv, st := newTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "ciba-user", store.CreateUserParams{
		DisplayName: "CIBA User",
		Email:       "ciba@example.com",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.CreateCIBARequest(ctx, store.CreateCIBARequestParams{
		AuthReqID:  "ciba-req-1",
		UserID:     "ciba-user",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
		WebhookURL: "",
	}); err != nil {
		t.Fatalf("create ciba request: %v", err)
	}
	if err := st.ApproveCIBARequest(ctx, "ciba-req-1"); err != nil {
		t.Fatalf("approve ciba request: %v", err)
	}

	body := `{"grant_type":"urn:openid:params:grant-type:ciba","auth_req_id":"ciba-req-1"}`

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/auth/token", strings.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRR := httptest.NewRecorder()
	srv.ServeHTTP(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first exchange status=%d want=200 body=%s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/auth/token", strings.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRR := httptest.NewRecorder()
	srv.ServeHTTP(secondRR, secondReq)
	if secondRR.Code != http.StatusBadRequest {
		t.Fatalf("second exchange status=%d want=400 body=%s", secondRR.Code, secondRR.Body.String())
	}
	if !strings.Contains(secondRR.Body.String(), "invalid_grant") {
		t.Fatalf("unexpected second exchange body: %s", secondRR.Body.String())
	}
}

func TestCIBAPollCanOnlyBeUsedOnce(t *testing.T) {
	t.Parallel()

	srv, st := newTestServer(t)
	ctx := context.Background()

	if _, err := st.CreateUser(ctx, "ciba-user-2", store.CreateUserParams{
		DisplayName: "CIBA User 2",
		Email:       "ciba2@example.com",
	}); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := st.CreateCIBARequest(ctx, store.CreateCIBARequestParams{
		AuthReqID:  "ciba-req-2",
		UserID:     "ciba-user-2",
		ClientID:   "cli",
		ExpiresAt:  time.Now().Add(5 * time.Minute).Format(time.RFC3339),
		Interval:   5,
		WebhookURL: "",
	}); err != nil {
		t.Fatalf("create ciba request: %v", err)
	}
	if err := st.ApproveCIBARequest(ctx, "ciba-req-2"); err != nil {
		t.Fatalf("approve ciba request: %v", err)
	}

	body := `{"auth_req_id":"ciba-req-2"}`

	firstReq := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba/:poll", strings.NewReader(body))
	firstReq.Header.Set("Content-Type", "application/json")
	firstRR := httptest.NewRecorder()
	srv.ServeHTTP(firstRR, firstReq)
	if firstRR.Code != http.StatusOK {
		t.Fatalf("first poll status=%d want=200 body=%s", firstRR.Code, firstRR.Body.String())
	}

	secondReq := httptest.NewRequest(http.MethodPost, "/v1/auth/ciba/:poll", strings.NewReader(body))
	secondReq.Header.Set("Content-Type", "application/json")
	secondRR := httptest.NewRecorder()
	srv.ServeHTTP(secondRR, secondReq)
	if secondRR.Code != http.StatusBadRequest {
		t.Fatalf("second poll status=%d want=400 body=%s", secondRR.Code, secondRR.Body.String())
	}
	if !strings.Contains(secondRR.Body.String(), "invalid_grant") {
		t.Fatalf("unexpected second poll body: %s", secondRR.Body.String())
	}
}

func TestAuthorizePageDisabledByDefault(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newSecuredTestServer(t)
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {"cli"},
		"redirect_uri":          {"http://localhost/callback"},
		"code_challenge":        {"abc123"},
		"code_challenge_method": {"S256"},
	}
	req := httptest.NewRequest(http.MethodGet, "/v1/auth/authorize?"+q.Encode(), nil)
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeviceFlowDisabledByDefault(t *testing.T) {
	t.Parallel()

	srv, _, _, _ := newSecuredTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/device", strings.NewReader(`{"client_id":"cli"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	srv.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", rr.Code, rr.Body.String())
	}
}
