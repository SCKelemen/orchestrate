package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/store"
)

// tokenRequest is the unified token endpoint request body.
// See RFC 6749 Section 4.1.3, RFC 8628 Section 3.4
type tokenRequest struct {
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token,omitempty"`
	// Bearer token exchange (backward compat)
	Token string `json:"token,omitempty"`
	// External IdP exchange (GitHub/Google)
	SubjectToken     string `json:"subject_token,omitempty"`
	SubjectTokenType string `json:"subject_token_type,omitempty"`
	// Device flow
	DeviceCode string `json:"device_code,omitempty"`
	// Auth code + PKCE
	Code         string `json:"code,omitempty"`
	RedirectURI  string `json:"redirect_uri,omitempty"`
	CodeVerifier string `json:"code_verifier,omitempty"`
	ClientID     string `json:"client_id,omitempty"`
	// CIBA
	AuthReqID string `json:"auth_req_id,omitempty"`
}

// tokenResponse is the standard OAuth 2.0 token response.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	UserID       string `json:"user_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
}

func (s *Server) registerAuthRoutes() {
	// Token endpoints (public - no auth required for obtaining tokens)
	s.mux.HandleFunc("POST /v1/auth/token", s.handleToken)
	s.mux.HandleFunc("POST /v1/auth/token/{action}", s.handleTokenAction)
	s.mux.HandleFunc("GET /v1/auth/userinfo", s.auth.Wrap(s.handleUserInfo))

	// Device flow endpoints
	s.mux.HandleFunc("POST /v1/auth/device", s.handleDeviceAuthorize)
	s.mux.HandleFunc("POST /v1/auth/device/{action}", s.handleDeviceAction)
	s.mux.HandleFunc("GET /v1/auth/device/verify", s.handleDeviceVerifyPage)
	s.mux.HandleFunc("POST /v1/auth/device/verify", s.handleDeviceVerifySubmit)

	// Authorization Code + PKCE endpoints
	s.mux.HandleFunc("GET /v1/auth/authorize", s.handleAuthorizePage)
	s.mux.HandleFunc("POST /v1/auth/authorize", s.handleAuthorizeSubmit)

	// CIBA endpoints
	s.mux.HandleFunc("POST /v1/auth/ciba", s.auth.Wrap(s.handleCIBAInitiate))
	s.mux.HandleFunc("POST /v1/auth/ciba/{action}", s.handleCIBAAction)

	// WebAuthn endpoints
	s.mux.HandleFunc("POST /v1/auth/webauthn/register/{action}", s.auth.Wrap(s.handleWebAuthnRegister))
	s.mux.HandleFunc("POST /v1/auth/webauthn/login/{action}", s.handleWebAuthnLogin)

	// User management
	s.mux.HandleFunc("POST /v1/users", s.auth.Wrap(s.createUser))
	s.mux.HandleFunc("GET /v1/users", s.auth.Wrap(s.listUsers))
	s.mux.HandleFunc("GET /v1/users/{user}", s.auth.Wrap(s.getUser))
	s.mux.HandleFunc("PATCH /v1/users/{user}", s.auth.Wrap(s.patchUser))
	s.mux.HandleFunc("DELETE /v1/users/{user}", s.auth.Wrap(s.deleteUser))
}

// handleToken is the unified token endpoint (POST /v1/auth/token).
// Dispatches on grant_type per RFC 6749 Section 4.
func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	switch req.GrantType {
	case "bearer_token":
		s.handleBearerTokenExchange(w, r, &req)
	case "refresh_token":
		s.handleRefreshToken(w, r, &req)
	case "urn:ietf:params:oauth:grant-type:token-exchange":
		s.handleExternalIdPExchange(w, r, &req)
	case "urn:ietf:params:oauth:grant-type:device_code":
		s.handleDeviceCodeGrant(w, r, &req)
	case "authorization_code":
		s.handleAuthCodeGrant(w, r, &req)
	case "urn:openid:params:grant-type:ciba":
		s.handleCIBAGrant(w, r, &req)
	default:
		writeError(w, http.StatusBadRequest, "unsupported grant_type: "+req.GrantType)
	}
}

// handleTokenAction dispatches token sub-actions (:refresh, :revoke).
func (s *Server) handleTokenAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case ":refresh":
		var req tokenRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		req.GrantType = "refresh_token"
		s.handleRefreshToken(w, r, &req)
	case ":revoke":
		s.handleRevokeToken(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

// handleBearerTokenExchange exchanges a static bearer token for JWT tokens.
// This provides backward compatibility: existing clients can exchange their
// static token for a JWT pair.
func (s *Server) handleBearerTokenExchange(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "token is required")
		return
	}

	// Validate the bearer token against the bearer provider.
	fakeReq, err := http.NewRequest("GET", "/", nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	fakeReq.Header.Set("Authorization", "Bearer "+req.Token)

	// Authenticate through configured providers.
	id, err := s.auth.Authenticate(fakeReq)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid bearer token")
		return
	}
	if id == nil || id.Provider != "bearer" {
		writeError(w, http.StatusUnauthorized, "invalid bearer token")
		return
	}

	resp, err := s.issueTokenPair(r, id)
	if err != nil {
		s.logger.Error("issue token pair", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRefreshToken exchanges a refresh token for a new access token.
// See RFC 6749 Section 6 - Refreshing an Access Token
// https://datatracker.ietf.org/doc/html/rfc6749#section-6
func (s *Server) handleRefreshToken(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	// Verify the refresh token JWT
	claims, err := s.signer.Verify(req.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "invalid refresh token")
		return
	}
	if claims.TokenType != "refresh" {
		writeError(w, http.StatusBadRequest, "not a refresh token")
		return
	}

	// Verify the session exists and is not revoked
	tokenHash := store.HashToken(req.RefreshToken)
	session, err := s.store.GetSessionByTokenHash(r.Context(), tokenHash)
	if err != nil || session == nil {
		writeError(w, http.StatusUnauthorized, "session not found or revoked")
		return
	}

	id := &auth.Identity{
		UserID:      claims.Subject,
		DisplayName: claims.DisplayName,
		Email:       claims.Email,
		Provider:    claims.Provider,
	}

	// Issue new access token only (refresh token stays the same)
	accessToken, err := s.signer.IssueAccessToken(id, newID())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue token")
		return
	}

	writeJSON(w, http.StatusOK, tokenResponse{
		AccessToken: accessToken,
		TokenType:   "Bearer",
		ExpiresIn:   int(auth.AccessTokenLifetime.Seconds()),
		UserID:      id.UserID,
		Provider:    id.Provider,
	})
}

// handleRevokeToken revokes a refresh token (logout).
func (s *Server) handleRevokeToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.RefreshToken == "" {
		writeError(w, http.StatusBadRequest, "refresh_token is required")
		return
	}

	tokenHash := store.HashToken(req.RefreshToken)
	session, err := s.store.GetSessionByTokenHash(r.Context(), tokenHash)
	if err != nil || session == nil {
		// Per RFC 7009, revocation of an invalid token is not an error
		w.WriteHeader(http.StatusOK)
		return
	}
	_ = s.store.RevokeSession(r.Context(), session.ID)
	w.WriteHeader(http.StatusOK)
}

// handleExternalIdPExchange validates an external IdP token and issues JWTs.
// See RFC 8693 - OAuth 2.0 Token Exchange
// https://datatracker.ietf.org/doc/html/rfc8693
func (s *Server) handleExternalIdPExchange(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	if req.SubjectToken == "" {
		writeError(w, http.StatusBadRequest, "subject_token is required")
		return
	}

	switch req.SubjectTokenType {
	case "urn:ietf:params:oauth:token-type:access_token":
		// Determine provider from subject_token_type or try GitHub first
		s.handleGitHubExchange(w, r, req.SubjectToken)
	case "github":
		s.handleGitHubExchange(w, r, req.SubjectToken)
	case "google":
		s.handleGoogleExchange(w, r, req.SubjectToken)
	default:
		writeError(w, http.StatusBadRequest, "unsupported subject_token_type: "+req.SubjectTokenType)
	}
}

// handleGitHubExchange validates a GitHub token and issues JWTs.
func (s *Server) handleGitHubExchange(w http.ResponseWriter, r *http.Request, token string) {
	gh := auth.NewGitHubExchange()
	user, err := gh.Exchange(token)
	if err != nil {
		s.logger.Error("github exchange", "error", err)
		writeError(w, http.StatusUnauthorized, "invalid github token")
		return
	}

	externalID := fmt.Sprintf("%d", user.ID)
	displayName := user.Name
	if displayName == "" {
		displayName = user.Login
	}

	// Look up or auto-provision user
	id, err := s.ensureExternalUser(r, "github", externalID, displayName, user.Email)
	if err != nil {
		s.logger.Error("ensure user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to provision user")
		return
	}

	resp, err := s.issueTokenPair(r, id)
	if err != nil {
		s.logger.Error("issue token pair", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGoogleExchange validates a Google token and issues JWTs.
func (s *Server) handleGoogleExchange(w http.ResponseWriter, r *http.Request, token string) {
	goog := auth.NewGoogleExchange()
	user, err := goog.Exchange(token)
	if err != nil {
		s.logger.Error("google exchange", "error", err)
		writeError(w, http.StatusUnauthorized, "invalid google token")
		return
	}

	displayName := user.Name

	// Look up or auto-provision user
	id, err := s.ensureExternalUser(r, "google", user.Sub, displayName, user.Email)
	if err != nil {
		s.logger.Error("ensure user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to provision user")
		return
	}

	resp, err := s.issueTokenPair(r, id)
	if err != nil {
		s.logger.Error("issue token pair", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleUserInfo returns the current authenticated user's identity.
func (s *Server) handleUserInfo(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "not authenticated")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"userId":      id.UserID,
		"displayName": id.DisplayName,
		"email":       id.Email,
		"provider":    id.Provider,
	})
}

// ensureExternalUser finds or creates a user for an external IdP credential.
func (s *Server) ensureExternalUser(r *http.Request, provider, externalID, displayName, email string) (*auth.Identity, error) {
	ctx := r.Context()

	// Check if credential already exists
	cred, err := s.store.GetCredentialByExternal(ctx, provider, externalID)
	if err != nil {
		return nil, err
	}
	if cred != nil {
		user, err := s.store.GetUser(ctx, cred.UserID)
		if err != nil {
			return nil, err
		}
		if user != nil {
			return &auth.Identity{
				UserID:      user.ID,
				DisplayName: user.DisplayName,
				Email:       user.Email,
				Provider:    provider,
			}, nil
		}
	}

	// Check if user with same email exists
	if email != "" {
		user, err := s.store.GetUserByEmail(ctx, email)
		if err != nil {
			return nil, err
		}
		if user != nil {
			// Link credential to existing user
			if _, err := s.store.CreateCredential(ctx, newID(), store.CreateCredentialParams{
				UserID:         user.ID,
				CredentialType: provider,
				ExternalID:     externalID,
			}); err != nil {
				return nil, err
			}
			return &auth.Identity{
				UserID:      user.ID,
				DisplayName: user.DisplayName,
				Email:       user.Email,
				Provider:    provider,
			}, nil
		}
	}

	// Create new user + credential
	userID := newID()
	user, err := s.store.CreateUser(ctx, userID, store.CreateUserParams{
		DisplayName: displayName,
		Email:       email,
	})
	if err != nil {
		return nil, err
	}
	if _, err := s.store.CreateCredential(ctx, newID(), store.CreateCredentialParams{
		UserID:         user.ID,
		CredentialType: provider,
		ExternalID:     externalID,
	}); err != nil {
		return nil, err
	}

	return &auth.Identity{
		UserID:      user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Provider:    provider,
	}, nil
}

// issueTokenPair creates an access + refresh token pair and records the session.
func (s *Server) issueTokenPair(r *http.Request, id *auth.Identity) (*tokenResponse, error) {
	accessJTI := newID()
	refreshJTI := newID()

	accessToken, err := s.signer.IssueAccessToken(id, accessJTI)
	if err != nil {
		return nil, err
	}
	refreshToken, err := s.signer.IssueRefreshToken(id, refreshJTI)
	if err != nil {
		return nil, err
	}

	// Record session
	expiresAt := time.Now().Add(auth.RefreshTokenLifetime).Format(time.RFC3339)
	if _, err := s.store.CreateSession(r.Context(), newID(), store.CreateSessionParams{
		UserID:       id.UserID,
		RefreshToken: refreshToken,
		Provider:     id.Provider,
		IPAddress:    r.RemoteAddr,
		UserAgent:    r.UserAgent(),
		ExpiresAt:    expiresAt,
	}); err != nil {
		return nil, err
	}

	return &tokenResponse{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		TokenType:    "Bearer",
		ExpiresIn:    int(auth.AccessTokenLifetime.Seconds()),
		UserID:       id.UserID,
		Provider:     id.Provider,
	}, nil
}

// Stub handlers for endpoints implemented in later phases

// handleDeviceAuthorize initiates the device authorization flow.
// POST /v1/auth/device
// RFC 8628 Section 3.1 - Device Authorization Request
// https://datatracker.ietf.org/doc/html/rfc8628#section-3.1
func (s *Server) handleDeviceAuthorize(w http.ResponseWriter, r *http.Request) {
	if !s.allowInsecureAuth {
		writeError(w, http.StatusForbidden, "device flow is disabled")
		return
	}

	var req struct {
		ClientID string `json:"client_id"`
		Scope    string `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	deviceCode, err := auth.GenerateDeviceCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	userCode, err := auth.GenerateUserCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	interval := 5
	expiresAt := time.Now().Add(5 * time.Minute).Format(time.RFC3339)

	if err := s.store.CreateDeviceCode(r.Context(), store.CreateDeviceCodeParams{
		DeviceCode: deviceCode,
		UserCode:   auth.NormalizeUserCode(userCode),
		ClientID:   req.ClientID,
		Scope:      req.Scope,
		ExpiresAt:  expiresAt,
		Interval:   interval,
	}); err != nil {
		s.logger.Error("create device code", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Determine verification URI from request host
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	verificationURI := fmt.Sprintf("%s://%s/v1/auth/device/verify", scheme, r.Host)

	// RFC 8628 Section 3.2 - Device Authorization Response
	// https://datatracker.ietf.org/doc/html/rfc8628#section-3.2
	writeJSON(w, http.StatusOK, map[string]any{
		"device_code":               deviceCode,
		"user_code":                 userCode,
		"verification_uri":          verificationURI,
		"verification_uri_complete": verificationURI + "?user_code=" + userCode,
		"expires_in":                300,
		"interval":                  interval,
	})
}

// handleDeviceAction dispatches device sub-actions (:poll).
func (s *Server) handleDeviceAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case ":poll":
		s.handleDevicePoll(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

// handleDevicePoll allows the CLI to poll for device code approval.
// POST /v1/auth/device/:poll
// RFC 8628 Section 3.4 - Device Access Token Request
// https://datatracker.ietf.org/doc/html/rfc8628#section-3.4
func (s *Server) handleDevicePoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		DeviceCode string `json:"device_code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "device_code is required")
		return
	}

	dc, err := s.store.GetDeviceCode(r.Context(), req.DeviceCode)
	if err != nil || dc == nil {
		writeError(w, http.StatusBadRequest, "invalid device_code")
		return
	}

	// Check expiry
	expiresAt, err := time.Parse(time.RFC3339, dc.ExpiresAt)
	if err != nil {
		s.logger.Error("parse device code expiry", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if time.Now().After(expiresAt) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
		return
	}

	switch dc.State {
	case store.DeviceCodePending:
		// RFC 8628 Section 3.5 - authorization_pending
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
	case store.DeviceCodeDenied:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access_denied"})
	case store.DeviceCodeApproved:
		if err := s.store.ConsumeDeviceCode(r.Context(), dc.DeviceCode); err != nil {
			if errors.Is(err, store.ErrDeviceCodeNotConsumable) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
				return
			}
			s.logger.Error("consume device code", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if dc.UserID == nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		user, err := s.store.GetUser(r.Context(), *dc.UserID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, "user not found")
			return
		}
		id := &auth.Identity{
			UserID:      user.ID,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Provider:    "device",
		}
		resp, err := s.issueTokenPair(r, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue tokens")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case store.DeviceCodeConsumed:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
	default:
		writeError(w, http.StatusInternalServerError, "unknown state")
	}
}

// handleDeviceVerifyPage renders the device code verification page.
// GET /v1/auth/device/verify
func (s *Server) handleDeviceVerifyPage(w http.ResponseWriter, r *http.Request) {
	if !s.allowInsecureAuth {
		writeError(w, http.StatusForbidden, "device flow is disabled")
		return
	}

	userCode := r.URL.Query().Get("user_code")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Device Verification</title>
<style>body{font-family:system-ui;max-width:400px;margin:80px auto;padding:0 20px}
input{width:100%%;padding:8px;margin:4px 0 12px;box-sizing:border-box;text-align:center;font-size:1.2em;letter-spacing:0.1em}
button{padding:10px 24px;cursor:pointer}</style></head>
<body><h2>Device Verification</h2>
<p>Enter the code shown on your device:</p>
<form method="POST" action="/v1/auth/device/verify">
<label>Code<input type="text" name="user_code" value="%s" maxlength="9" pattern="[A-Za-z0-9]{4}-?[A-Za-z0-9]{4}" required placeholder="XXXX-XXXX"></label>
<label>Email<input type="email" name="email" required></label>
<label>Display Name<input type="text" name="display_name"></label>
<button type="submit" name="action" value="approve">Approve</button>
<button type="submit" name="action" value="deny">Deny</button>
</form></body></html>`, html.EscapeString(userCode))
}

// handleDeviceVerifySubmit processes the device code verification form.
// POST /v1/auth/device/verify
func (s *Server) handleDeviceVerifySubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allowInsecureAuth {
		writeError(w, http.StatusForbidden, "device flow is disabled")
		return
	}

	if err := parseFormWithBodyLimit(w, r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}

	userCode := auth.NormalizeUserCode(r.FormValue("user_code")) // #nosec G120 -- body limited by parseFormWithBodyLimit above
	action := r.FormValue("action")                              // #nosec G120
	email := r.FormValue("email")                                // #nosec G120
	displayName := r.FormValue("display_name")                   // #nosec G120

	if userCode == "" {
		writeError(w, http.StatusBadRequest, "user_code is required")
		return
	}
	if action != "approve" && action != "deny" {
		writeError(w, http.StatusBadRequest, "action must be approve or deny")
		return
	}
	if action == "approve" && email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	dc, err := s.store.GetDeviceCodeByUserCode(r.Context(), userCode)
	if err != nil || dc == nil {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Invalid Code</h2><p>The code was not found or has expired. Please try again.</p></body></html>`)
		return
	}

	// Check expiry
	expiresAt, err := time.Parse(time.RFC3339, dc.ExpiresAt)
	if err != nil {
		s.logger.Error("parse device code expiry", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if time.Now().After(expiresAt) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Code Expired</h2><p>This code has expired. Please request a new one.</p></body></html>`)
		return
	}

	if action == "deny" {
		_ = s.store.DenyDeviceCode(r.Context(), dc.DeviceCode)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Denied</h2><p>Authorization was denied.</p></body></html>`)
		return
	}

	// Approve: find or create user
	ctx := r.Context()
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil {
		userID := newID()
		user, err = s.store.CreateUser(ctx, userID, store.CreateUserParams{
			DisplayName: displayName,
			Email:       email,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	if err := s.store.ApproveDeviceCode(ctx, dc.DeviceCode, user.ID); err != nil {
		s.logger.Error("approve device code", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Approved!</h2><p>You have authorized the device. You can close this tab.</p></body></html>`)
}

// handleDeviceCodeGrant exchanges an approved device code for tokens.
// Called from the unified token endpoint with grant_type=urn:ietf:params:oauth:grant-type:device_code
func (s *Server) handleDeviceCodeGrant(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	if req.DeviceCode == "" {
		writeError(w, http.StatusBadRequest, "device_code is required")
		return
	}

	dc, err := s.store.GetDeviceCode(r.Context(), req.DeviceCode)
	if err != nil || dc == nil {
		writeError(w, http.StatusBadRequest, "invalid device_code")
		return
	}

	expiresAt, err := time.Parse(time.RFC3339, dc.ExpiresAt)
	if err != nil {
		s.logger.Error("parse device code expiry", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if time.Now().After(expiresAt) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
		return
	}

	switch dc.State {
	case store.DeviceCodePending:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
	case store.DeviceCodeDenied:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access_denied"})
	case store.DeviceCodeApproved:
		if err := s.store.ConsumeDeviceCode(r.Context(), dc.DeviceCode); err != nil {
			if errors.Is(err, store.ErrDeviceCodeNotConsumable) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
				return
			}
			s.logger.Error("consume device code", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		if dc.UserID == nil {
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		user, err := s.store.GetUser(r.Context(), *dc.UserID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, "user not found")
			return
		}
		id := &auth.Identity{
			UserID:      user.ID,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Provider:    "device",
		}
		resp, err := s.issueTokenPair(r, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue tokens")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case store.DeviceCodeConsumed:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
	default:
		writeError(w, http.StatusInternalServerError, "unknown state")
	}
}

// handleAuthorizePage renders a minimal login form.
// GET /v1/auth/authorize?response_type=code&client_id=...&redirect_uri=...&code_challenge=...&code_challenge_method=S256&state=...
func (s *Server) handleAuthorizePage(w http.ResponseWriter, r *http.Request) {
	if !s.allowInsecureAuth {
		writeError(w, http.StatusForbidden, "authorization code login flow is disabled")
		return
	}

	q := r.URL.Query()
	if q.Get("response_type") != "code" {
		writeError(w, http.StatusBadRequest, "response_type must be 'code'")
		return
	}
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		writeError(w, http.StatusBadRequest, "PKCE S256 required")
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, `<!DOCTYPE html>
<html><head><title>Orchestrate Login</title>
<style>body{font-family:system-ui;max-width:400px;margin:80px auto;padding:0 20px}
input{width:100%%;padding:8px;margin:4px 0 12px;box-sizing:border-box}
button{padding:10px 24px;cursor:pointer}</style></head>
<body><h2>Orchestrate Login</h2>
<form method="POST" action="/v1/auth/authorize">
<input type="hidden" name="response_type" value="%s">
<input type="hidden" name="client_id" value="%s">
<input type="hidden" name="redirect_uri" value="%s">
<input type="hidden" name="code_challenge" value="%s">
<input type="hidden" name="code_challenge_method" value="%s">
<input type="hidden" name="state" value="%s">
<input type="hidden" name="scope" value="%s">
<label>Email<input type="email" name="email" required></label>
<label>Display Name<input type="text" name="display_name"></label>
<button type="submit">Login</button>
</form></body></html>`,
		html.EscapeString(q.Get("response_type")),
		html.EscapeString(q.Get("client_id")),
		html.EscapeString(q.Get("redirect_uri")),
		html.EscapeString(q.Get("code_challenge")),
		html.EscapeString(q.Get("code_challenge_method")),
		html.EscapeString(q.Get("state")),
		html.EscapeString(q.Get("scope")))
}

// handleAuthorizeSubmit processes the login form and redirects with an auth code.
// POST /v1/auth/authorize (form data)
func (s *Server) handleAuthorizeSubmit(w http.ResponseWriter, r *http.Request) {
	if !s.allowInsecureAuth {
		writeError(w, http.StatusForbidden, "authorization code login flow is disabled")
		return
	}

	if err := parseFormWithBodyLimit(w, r); err != nil {
		writeError(w, http.StatusBadRequest, "invalid form data")
		return
	}

	email := r.FormValue("email")                               // #nosec G120 -- body limited by parseFormWithBodyLimit above
	displayName := r.FormValue("display_name")                  // #nosec G120
	redirectURI := r.FormValue("redirect_uri")                  // #nosec G120
	codeChallenge := r.FormValue("code_challenge")              // #nosec G120
	codeChallengeMethod := r.FormValue("code_challenge_method") // #nosec G120
	state := r.FormValue("state")                               // #nosec G120
	clientID := r.FormValue("client_id")                        // #nosec G120
	scope := r.FormValue("scope")                               // #nosec G120

	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if redirectURI == "" {
		writeError(w, http.StatusBadRequest, "redirect_uri is required")
		return
	}
	if clientID == "" {
		writeError(w, http.StatusBadRequest, "client_id is required")
		return
	}
	if err := validateRedirectURI(redirectURI); err != nil {
		writeError(w, http.StatusBadRequest, "invalid redirect_uri: "+err.Error())
		return
	}
	if codeChallengeMethod != "S256" {
		writeError(w, http.StatusBadRequest, "only S256 code_challenge_method is supported")
		return
	}
	if codeChallenge == "" {
		writeError(w, http.StatusBadRequest, "code_challenge is required")
		return
	}

	// Find or create user by email
	ctx := r.Context()
	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		s.logger.Error("get user by email", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil {
		userID := newID()
		user, err = s.store.CreateUser(ctx, userID, store.CreateUserParams{
			DisplayName: displayName,
			Email:       email,
		})
		if err != nil {
			s.logger.Error("create user", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	// Generate auth code
	code, err := auth.GenerateAuthCode()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	expiresAt := time.Now().Add(10 * time.Minute).Format(time.RFC3339)
	if err := s.store.CreateAuthCode(ctx, code, store.CreateAuthCodeParams{
		UserID:              user.ID,
		ClientID:            clientID,
		RedirectURI:         redirectURI,
		Scope:               scope,
		CodeChallenge:       codeChallenge,
		CodeChallengeMethod: codeChallengeMethod,
		ExpiresAt:           expiresAt,
	}); err != nil {
		s.logger.Error("create auth code", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Redirect with code/state via proper URL query encoding.
	redirectURL, err := url.Parse(redirectURI)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid redirect_uri")
		return
	}
	query := redirectURL.Query()
	query.Set("code", code)
	if state != "" {
		query.Set("state", state)
	}
	redirectURL.RawQuery = query.Encode()
	http.Redirect(w, r, redirectURL.String(), http.StatusFound)
}

// handleAuthCodeGrant exchanges an authorization code + PKCE verifier for tokens.
// RFC 6749 Section 4.1.3 + RFC 7636
func (s *Server) handleAuthCodeGrant(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	if req.Code == "" {
		writeError(w, http.StatusBadRequest, "code is required")
		return
	}
	if req.CodeVerifier == "" {
		writeError(w, http.StatusBadRequest, "code_verifier is required")
		return
	}
	if req.RedirectURI == "" {
		writeError(w, http.StatusBadRequest, "redirect_uri is required")
		return
	}
	if req.ClientID == "" {
		writeError(w, http.StatusBadRequest, "client_id is required")
		return
	}

	ctx := r.Context()
	ac, err := s.store.GetAuthCode(ctx, req.Code)
	if err != nil || ac == nil {
		writeError(w, http.StatusBadRequest, "invalid authorization code")
		return
	}

	// Check not consumed
	if ac.Consumed {
		writeError(w, http.StatusBadRequest, "authorization code already used")
		return
	}

	// Check not expired
	expiresAt, err := time.Parse(time.RFC3339, ac.ExpiresAt)
	if err != nil || time.Now().After(expiresAt) {
		writeError(w, http.StatusBadRequest, "authorization code expired")
		return
	}

	// Verify client and redirect URI match the authorization request.
	if req.ClientID != ac.ClientID {
		writeError(w, http.StatusBadRequest, "client_id mismatch")
		return
	}
	if req.RedirectURI != ac.RedirectURI {
		writeError(w, http.StatusBadRequest, "redirect_uri mismatch")
		return
	}

	// Verify PKCE
	if err := auth.VerifyPKCE(ac.CodeChallengeMethod, ac.CodeChallenge, req.CodeVerifier); err != nil {
		writeError(w, http.StatusBadRequest, "PKCE verification failed")
		return
	}

	// Consume the code
	if err := s.store.ConsumeAuthCode(ctx, req.Code); err != nil {
		writeError(w, http.StatusBadRequest, "authorization code already used")
		return
	}

	// Look up user
	user, err := s.store.GetUser(ctx, ac.UserID)
	if err != nil || user == nil {
		writeError(w, http.StatusInternalServerError, "user not found")
		return
	}

	id := &auth.Identity{
		UserID:      user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Provider:    "authcode",
	}

	resp, err := s.issueTokenPair(r, id)
	if err != nil {
		s.logger.Error("issue token pair", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleCIBAInitiate starts a CIBA backchannel authentication request.
// POST /v1/auth/ciba
// OpenID Connect CIBA Section 7 - Authentication Request
// https://openid.net/specs/openid-client-initiated-backchannel-authentication-core-1_0.html#rfc.section.7
func (s *Server) handleCIBAInitiate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		LoginHint      string `json:"login_hint"`
		Scope          string `json:"scope"`
		BindingMessage string `json:"binding_message"`
		ClientID       string `json:"client_id"`
		WebhookURL     string `json:"webhook_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.LoginHint == "" {
		writeError(w, http.StatusBadRequest, "login_hint is required (user email)")
		return
	}
	if req.WebhookURL != "" {
		if err := validateWebhookURL(req.WebhookURL); err != nil {
			writeError(w, http.StatusBadRequest, "invalid webhook_url: "+err.Error())
			return
		}
	}

	// Find user by login_hint (email)
	user, err := s.store.GetUserByEmail(r.Context(), req.LoginHint)
	if err != nil || user == nil {
		writeError(w, http.StatusBadRequest, "user not found for login_hint")
		return
	}

	authReqID, err := auth.GenerateAuthReqID()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	interval := 5
	expiresIn := 300
	expiresAt := time.Now().Add(time.Duration(expiresIn) * time.Second).Format(time.RFC3339)

	if err := s.store.CreateCIBARequest(r.Context(), store.CreateCIBARequestParams{
		AuthReqID:  authReqID,
		UserID:     user.ID,
		ClientID:   req.ClientID,
		Scope:      req.Scope,
		BindingMsg: req.BindingMessage,
		ExpiresAt:  expiresAt,
		Interval:   interval,
		WebhookURL: req.WebhookURL,
	}); err != nil {
		s.logger.Error("create ciba request", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}

	// Fire webhook notification if configured — intentionally detached from request context
	// so the webhook delivery is not cancelled when the HTTP response completes.
	if req.WebhookURL != "" {
		go s.fireCIBAWebhook(req.WebhookURL, authReqID, req.LoginHint, req.BindingMessage) // #nosec G118
	}

	// CIBA Section 7.3 - Successful Authentication Response
	writeJSON(w, http.StatusOK, map[string]any{
		"auth_req_id": authReqID,
		"expires_in":  expiresIn,
		"interval":    interval,
	})
}

// handleCIBAAction dispatches CIBA sub-actions (:poll, :approve, :deny).
func (s *Server) handleCIBAAction(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case ":poll":
		s.handleCIBAPoll(w, r)
	case ":approve":
		s.handleCIBAApprove(w, r)
	case ":deny":
		s.handleCIBADeny(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

// handleCIBAPoll allows polling for CIBA request approval.
// POST /v1/auth/ciba/:poll
func (s *Server) handleCIBAPoll(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AuthReqID string `json:"auth_req_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.AuthReqID == "" {
		writeError(w, http.StatusBadRequest, "auth_req_id is required")
		return
	}

	cr, err := s.store.GetCIBARequest(r.Context(), req.AuthReqID)
	if err != nil || cr == nil {
		writeError(w, http.StatusBadRequest, "invalid auth_req_id")
		return
	}

	expiresAt, err := time.Parse(time.RFC3339, cr.ExpiresAt)
	if err != nil {
		s.logger.Error("parse ciba request expiry", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if time.Now().After(expiresAt) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
		return
	}

	switch cr.State {
	case store.CIBAPending:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
	case store.CIBADenied:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access_denied"})
	case store.CIBAApproved:
		if err := s.store.ConsumeCIBARequest(r.Context(), cr.AuthReqID); err != nil {
			if errors.Is(err, store.ErrCIBARequestNotConsumable) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
				return
			}
			s.logger.Error("consume ciba request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		user, err := s.store.GetUser(r.Context(), cr.UserID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, "user not found")
			return
		}
		id := &auth.Identity{
			UserID:      user.ID,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Provider:    "ciba",
		}
		resp, err := s.issueTokenPair(r, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue tokens")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case store.CIBAConsumed:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
	default:
		writeError(w, http.StatusInternalServerError, "unknown state")
	}
}

// handleCIBAApprove approves a pending CIBA request.
// POST /v1/auth/ciba/:approve
func (s *Server) handleCIBAApprove(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AuthReqID string `json:"auth_req_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.AuthReqID == "" {
		writeError(w, http.StatusBadRequest, "auth_req_id is required")
		return
	}
	id, err := s.auth.Authenticate(r)
	if err != nil || id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	cr, err := s.store.GetCIBARequest(r.Context(), req.AuthReqID)
	if err != nil || cr == nil {
		writeError(w, http.StatusBadRequest, "invalid auth_req_id")
		return
	}
	if !isAdminIdentity(id) && id.UserID != cr.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := s.store.ApproveCIBARequest(r.Context(), req.AuthReqID); err != nil {
		writeError(w, http.StatusBadRequest, "failed to approve: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "approved"})
}

// handleCIBADeny denies a pending CIBA request.
// POST /v1/auth/ciba/:deny
func (s *Server) handleCIBADeny(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AuthReqID string `json:"auth_req_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.AuthReqID == "" {
		writeError(w, http.StatusBadRequest, "auth_req_id is required")
		return
	}
	id, err := s.auth.Authenticate(r)
	if err != nil || id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	cr, err := s.store.GetCIBARequest(r.Context(), req.AuthReqID)
	if err != nil || cr == nil {
		writeError(w, http.StatusBadRequest, "invalid auth_req_id")
		return
	}
	if !isAdminIdentity(id) && id.UserID != cr.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := s.store.DenyCIBARequest(r.Context(), req.AuthReqID); err != nil {
		writeError(w, http.StatusBadRequest, "failed to deny")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "denied"})
}

// handleCIBAGrant exchanges an approved CIBA request for tokens.
func (s *Server) handleCIBAGrant(w http.ResponseWriter, r *http.Request, req *tokenRequest) {
	if req.AuthReqID == "" {
		writeError(w, http.StatusBadRequest, "auth_req_id is required")
		return
	}

	cr, err := s.store.GetCIBARequest(r.Context(), req.AuthReqID)
	if err != nil || cr == nil {
		writeError(w, http.StatusBadRequest, "invalid auth_req_id")
		return
	}

	expiresAt, err := time.Parse(time.RFC3339, cr.ExpiresAt)
	if err != nil {
		s.logger.Error("parse ciba request expiry", "error", err)
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	if time.Now().After(expiresAt) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "expired_token"})
		return
	}

	switch cr.State {
	case store.CIBAPending:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "authorization_pending"})
	case store.CIBADenied:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "access_denied"})
	case store.CIBAApproved:
		if err := s.store.ConsumeCIBARequest(r.Context(), cr.AuthReqID); err != nil {
			if errors.Is(err, store.ErrCIBARequestNotConsumable) {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
				return
			}
			s.logger.Error("consume ciba request", "error", err)
			writeError(w, http.StatusInternalServerError, "internal error")
			return
		}
		user, err := s.store.GetUser(r.Context(), cr.UserID)
		if err != nil || user == nil {
			writeError(w, http.StatusInternalServerError, "user not found")
			return
		}
		id := &auth.Identity{
			UserID:      user.ID,
			DisplayName: user.DisplayName,
			Email:       user.Email,
			Provider:    "ciba",
		}
		resp, err := s.issueTokenPair(r, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to issue tokens")
			return
		}
		writeJSON(w, http.StatusOK, resp)
	case store.CIBAConsumed:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
	default:
		writeError(w, http.StatusInternalServerError, "unknown state")
	}
}

// fireCIBAWebhook sends a notification to the configured webhook URL.
func (s *Server) fireCIBAWebhook(webhookURL, authReqID, loginHint, bindingMsg string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	u, err := url.Parse(webhookURL)
	if err != nil {
		s.logger.Error("ciba webhook", "error", err, "url", webhookURL)
		return
	}
	if err := validateWebhookDispatchTarget(ctx, u); err != nil {
		s.logger.Error("ciba webhook blocked", "error", err, "url", webhookURL)
		return
	}

	body, err := json.Marshal(map[string]string{
		"auth_req_id":     authReqID,
		"login_hint":      loginHint,
		"binding_message": bindingMsg,
	})
	if err != nil {
		s.logger.Error("ciba webhook", "error", err, "url", webhookURL)
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		s.logger.Error("ciba webhook", "error", err, "url", webhookURL)
		return
	}
	req.Header.Set("Content-Type", "application/json")

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	transport := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			if err := validateWebhookResolvedHost(ctx, host); err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, network, net.JoinHostPort(host, port))
		},
	}
	defer transport.CloseIdleConnections()

	client := &http.Client{
		Timeout:   5 * time.Second,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		s.logger.Error("ciba webhook", "error", err, "url", webhookURL)
		return
	}
	_ = resp.Body.Close()
}

// handleWebAuthnRegister dispatches WebAuthn registration ceremony actions.
// POST /v1/auth/webauthn/register/:begin and :finish
func (s *Server) handleWebAuthnRegister(w http.ResponseWriter, r *http.Request) {
	if s.webauthn == nil {
		writeError(w, http.StatusNotImplemented, "webauthn not configured")
		return
	}

	action := r.PathValue("action")
	switch action {
	case ":begin":
		s.handleWebAuthnRegisterBegin(w, r)
	case ":finish":
		s.handleWebAuthnRegisterFinish(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

// handleWebAuthnLogin dispatches WebAuthn login ceremony actions.
// POST /v1/auth/webauthn/login/:begin and :finish
func (s *Server) handleWebAuthnLogin(w http.ResponseWriter, r *http.Request) {
	if s.webauthn == nil {
		writeError(w, http.StatusNotImplemented, "webauthn not configured")
		return
	}

	action := r.PathValue("action")
	switch action {
	case ":begin":
		s.handleWebAuthnLoginBegin(w, r)
	case ":finish":
		s.handleWebAuthnLoginFinish(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

func (s *Server) handleWebAuthnRegisterBegin(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	user, err := s.store.GetUser(r.Context(), id.UserID)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	// Build WebAuthn user with existing credentials
	wanUser, err := s.buildWebAuthnUser(r.Context(), user)
	if err != nil {
		s.logger.Error("build webauthn user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load credentials")
		return
	}

	creation, session, err := s.webauthn.BeginRegistration(wanUser)
	if err != nil {
		s.logger.Error("webauthn begin registration", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to begin registration")
		return
	}

	// Store session for :finish
	s.webauthnSessions.Save("reg:"+id.UserID, session)

	writeJSON(w, http.StatusOK, creation)
}

func (s *Server) handleWebAuthnRegisterFinish(w http.ResponseWriter, r *http.Request) {
	id := auth.FromContext(r.Context())
	if id == nil {
		writeError(w, http.StatusUnauthorized, "authentication required")
		return
	}

	session, ok := s.webauthnSessions.Get("reg:" + id.UserID)
	if !ok {
		writeError(w, http.StatusBadRequest, "no pending registration")
		return
	}

	user, err := s.store.GetUser(r.Context(), id.UserID)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	wanUser, err := s.buildWebAuthnUser(r.Context(), user)
	if err != nil {
		s.logger.Error("build webauthn user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load credentials")
		return
	}

	cred, err := s.webauthn.FinishRegistration(wanUser, *session, r)
	if err != nil {
		s.logger.Error("webauthn finish registration", "error", err)
		writeError(w, http.StatusBadRequest, "registration verification failed")
		return
	}

	// Store the credential
	credJSON, err := auth.MarshalCredential(cred)
	if err != nil {
		s.logger.Error("marshal webauthn credential", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store credential")
		return
	}
	if _, err := s.store.CreateCredential(r.Context(), newID(), store.CreateCredentialParams{
		UserID:         id.UserID,
		CredentialType: "webauthn",
		ExternalID:     fmt.Sprintf("%x", cred.ID),
		PublicKey:      credJSON,
	}); err != nil {
		s.logger.Error("store webauthn credential", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store credential")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "registered"})
}

func (s *Server) handleWebAuthnLoginBegin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.Email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}

	user, err := s.store.GetUserByEmail(r.Context(), req.Email)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	wanUser, err := s.buildWebAuthnUser(r.Context(), user)
	if err != nil {
		s.logger.Error("build webauthn user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load credentials")
		return
	}
	if len(wanUser.Credentials) == 0 {
		writeError(w, http.StatusBadRequest, "no webauthn credentials registered")
		return
	}

	assertion, session, err := s.webauthn.BeginLogin(wanUser)
	if err != nil {
		s.logger.Error("webauthn begin login", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to begin login")
		return
	}

	s.webauthnSessions.Save("login:"+user.ID, session)

	writeJSON(w, http.StatusOK, map[string]any{
		"assertion": assertion,
		"userId":    user.ID,
	})
}

func (s *Server) handleWebAuthnLoginFinish(w http.ResponseWriter, r *http.Request) {
	userID := r.URL.Query().Get("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id query parameter is required")
		return
	}

	session, ok := s.webauthnSessions.Get("login:" + userID)
	if !ok {
		writeError(w, http.StatusBadRequest, "no pending login")
		return
	}

	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "user not found")
		return
	}

	wanUser, err := s.buildWebAuthnUser(r.Context(), user)
	if err != nil {
		s.logger.Error("build webauthn user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load credentials")
		return
	}

	_, err = s.webauthn.FinishLogin(wanUser, *session, r)
	if err != nil {
		s.logger.Error("webauthn finish login", "error", err)
		writeError(w, http.StatusUnauthorized, "login verification failed")
		return
	}

	identity := &auth.Identity{
		UserID:      user.ID,
		DisplayName: user.DisplayName,
		Email:       user.Email,
		Provider:    "webauthn",
	}

	resp, err := s.issueTokenPair(r, identity)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to issue tokens")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) buildWebAuthnUser(ctx context.Context, user *store.User) (*auth.WebAuthnUser, error) {
	wanUser := &auth.WebAuthnUser{
		ID:          []byte(user.ID),
		Name:        user.Email,
		DisplayName: user.DisplayName,
	}

	creds, err := s.store.ListCredentials(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	for _, c := range creds {
		if c.CredentialType != "webauthn" || len(c.PublicKey) == 0 {
			continue
		}
		wanCred, err := auth.UnmarshalCredential(c.PublicKey)
		if err == nil {
			wanUser.Credentials = append(wanUser.Credentials, *wanCred)
		}
	}

	return wanUser, nil
}

// User management handlers

type createUserRequest struct {
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
}

type userResponse struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Email       string `json:"email"`
	State       string `json:"state"`
	CreateTime  string `json:"createTime"`
	UpdateTime  string `json:"updateTime"`
}

func toUserResponse(u *store.User) userResponse {
	return userResponse{
		Name:        "users/" + u.ID,
		DisplayName: u.DisplayName,
		Email:       u.Email,
		State:       u.State,
		CreateTime:  u.CreateTime,
		UpdateTime:  u.UpdateTime,
	}
}

func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if !isAdminIdentity(auth.FromContext(r.Context())) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	var req createUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	user, err := s.store.CreateUser(r.Context(), newID(), store.CreateUserParams{
		DisplayName: req.DisplayName,
		Email:       req.Email,
	})
	if err != nil {
		s.logger.Error("create user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	writeJSON(w, http.StatusCreated, toUserResponse(user))
}

func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !isAdminIdentity(auth.FromContext(r.Context())) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	q := r.URL.Query()
	pageSize := 20
	if ps := q.Get("pageSize"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			pageSize = n
		}
	}

	users, err := s.store.ListUsers(r.Context(), store.ListUsersParams{
		PageSize:  pageSize,
		PageToken: q.Get("pageToken"),
	})
	if err != nil {
		s.logger.Error("list users", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list users")
		return
	}

	resp := make([]userResponse, 0, len(users))
	var nextPageToken string
	for i, u := range users {
		if i >= pageSize {
			nextPageToken = u.ID
			break
		}
		resp = append(resp, toUserResponse(u))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"users":         resp,
		"nextPageToken": nextPageToken,
	})
}

func (s *Server) getUser(w http.ResponseWriter, r *http.Request) {
	if !isAdminIdentity(auth.FromContext(r.Context())) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	id := r.PathValue("user")
	user, err := s.store.GetUser(r.Context(), id)
	if err != nil {
		s.logger.Error("get user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get user")
		return
	}
	if user == nil {
		writeError(w, http.StatusNotFound, "user not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (s *Server) patchUser(w http.ResponseWriter, r *http.Request) {
	if !isAdminIdentity(auth.FromContext(r.Context())) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	id := r.PathValue("user")
	var req struct {
		DisplayName *string `json:"displayName"`
		Email       *string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if err := s.store.UpdateUser(r.Context(), id, req.DisplayName, req.Email); err != nil {
		s.logger.Error("update user", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update user")
		return
	}
	user, err := s.store.GetUser(r.Context(), id)
	if err != nil || user == nil {
		writeError(w, http.StatusNotFound, "user not found: "+id)
		return
	}
	writeJSON(w, http.StatusOK, toUserResponse(user))
}

func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !isAdminIdentity(auth.FromContext(r.Context())) {
		writeError(w, http.StatusForbidden, "forbidden")
		return
	}
	id := r.PathValue("user")
	if err := s.store.DeleteUser(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, "user not found: "+id)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func isAdminIdentity(id *auth.Identity) bool {
	// Current policy: static bearer identity is admin.
	return id != nil && id.Provider == "bearer"
}

func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme must be http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if u.Scheme == "http" {
		if strings.EqualFold(host, "localhost") {
			return nil
		}
		ip := net.ParseIP(host)
		if ip == nil || !ip.IsLoopback() {
			return fmt.Errorf("http redirect_uri must be localhost or loopback IP")
		}
	}
	return nil
}

func validateWebhookURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	if u.Scheme != "https" {
		return fmt.Errorf("scheme must be https")
	}
	if u.Host == "" {
		return fmt.Errorf("host is required")
	}
	if u.User != nil {
		return fmt.Errorf("userinfo is not allowed")
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return fmt.Errorf("host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return fmt.Errorf("localhost targets are not allowed")
	}
	if ip := net.ParseIP(host); ip != nil {
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("private or local IP targets are not allowed")
		}
	}
	return nil
}

func validateWebhookDispatchTarget(ctx context.Context, u *url.URL) error {
	if err := validateWebhookURL(u.String()); err != nil {
		return err
	}
	return validateWebhookResolvedHost(ctx, u.Hostname())
}

func validateWebhookResolvedHost(ctx context.Context, host string) error {
	if host == "" {
		return fmt.Errorf("host is required")
	}

	if ip := net.ParseIP(host); ip != nil {
		if isDisallowedWebhookIP(ip) {
			return fmt.Errorf("private or local IP targets are not allowed")
		}
		return nil
	}

	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve host: %w", err)
	}
	if len(addrs) == 0 {
		return fmt.Errorf("host has no resolved IPs")
	}
	for _, addr := range addrs {
		if isDisallowedWebhookIP(addr.IP) {
			return fmt.Errorf("private or local IP targets are not allowed")
		}
	}
	return nil
}

func isDisallowedWebhookIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return true
	}

	if v4 := ip.To4(); v4 != nil {
		// Block CGNAT and other non-routable IPv4 ranges not covered by net.IP helpers above.
		if v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
			return true
		}
		if v4[0] == 0 {
			return true
		}
	}

	return false
}
