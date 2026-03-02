package command

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/SCKelemen/clix/v2"
	"github.com/SCKelemen/orchestrate/internal/auth"
)

func newAuthCmd() *clix.Command {
	cmd := clix.NewCommand("auth")
	cmd.Short = "Manage authentication"

	cmd.Children = []*clix.Command{
		newAuthLoginCmd(),
		newAuthLogoutCmd(),
		newAuthStatusCmd(),
		newAuthTokenCmd(),
	}

	return cmd
}

func newAuthLoginCmd() *clix.Command {
	cmd := clix.NewCommand("login")
	cmd.Short = "Authenticate with the server"

	var (
		cc     ClientConfig
		method string
		token  string
	)

	cc.RegisterFlags(cmd)
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "method", Short: "m"},
		Default:     "token",
		Value:       &method,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "with-token"},
		Value:       &token,
	})

	cmd.Run = func(ctx *clix.Context) error {
		switch method {
		case "token":
			return loginWithToken(ctx, &cc, token)
		case "github":
			return loginWithGitHub(ctx, &cc, token)
		case "browser":
			return loginWithBrowser(ctx, &cc)
		case "device":
			return loginWithDevice(ctx, &cc)
		case "google":
			return loginWithGoogle(ctx, &cc, token)
		default:
			return fmt.Errorf("unknown method: %s (valid: token, github, browser, device, google)", method)
		}
	}

	return cmd
}

func loginWithToken(ctx *clix.Context, cc *ClientConfig, token string) error {
	if token == "" {
		return fmt.Errorf("--with-token is required for token login")
	}

	body := fmt.Sprintf(`{"grant_type":"bearer_token","token":"%s"}`, token)
	resp, err := apiRequest(cc.ServerURL, "", "POST", "/v1/auth/token", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		UserID       string `json:"user_id"`
		Provider     string `json:"provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	creds := &Credentials{
		Server:       cc.ServerURL,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
		UserID:       result.UserID,
		Provider:     result.Provider,
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	_, _ = fmt.Fprintf(ctx.App.Out, "Logged in as %s (provider: %s)\n", result.UserID, result.Provider)
	return nil
}

func loginWithGitHub(ctx *clix.Context, cc *ClientConfig, token string) error {
	if token == "" {
		return fmt.Errorf("--with-token is required for github login (provide a GitHub personal access token)")
	}

	body := fmt.Sprintf(`{"grant_type":"urn:ietf:params:oauth:grant-type:token-exchange","subject_token":"%s","subject_token_type":"github"}`, token)
	resp, err := apiRequest(cc.ServerURL, "", "POST", "/v1/auth/token", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("login failed: %s", errResp.Error.Message)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		UserID       string `json:"user_id"`
		Provider     string `json:"provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	creds := &Credentials{
		Server:       cc.ServerURL,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
		UserID:       result.UserID,
		Provider:     result.Provider,
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	_, _ = fmt.Fprintf(ctx.App.Out, "Logged in as %s (provider: %s)\n", result.UserID, result.Provider)
	return nil
}

func loginWithGoogle(ctx *clix.Context, cc *ClientConfig, token string) error {
	if token == "" {
		return fmt.Errorf("--with-token is required for google login (provide a Google access token)")
	}

	body := fmt.Sprintf(`{"grant_type":"urn:ietf:params:oauth:grant-type:token-exchange","subject_token":"%s","subject_token_type":"google"}`, token)
	resp, err := apiRequest(cc.ServerURL, "", "POST", "/v1/auth/token", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("login failed: %s", errResp.Error.Message)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		UserID       string `json:"user_id"`
		Provider     string `json:"provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	creds := &Credentials{
		Server:       cc.ServerURL,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
		UserID:       result.UserID,
		Provider:     result.Provider,
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	_, _ = fmt.Fprintf(ctx.App.Out, "Logged in as %s (provider: %s)\n", result.UserID, result.Provider)
	return nil
}

func newAuthLogoutCmd() *clix.Command {
	cmd := clix.NewCommand("logout")
	cmd.Short = "Revoke session and remove stored credentials"

	var cc ClientConfig
	cc.RegisterFlags(cmd)

	cmd.Run = func(ctx *clix.Context) error {
		creds, err := LoadCredentials()
		if err != nil {
			// No credentials stored, nothing to do
			_, _ = fmt.Fprintln(ctx.App.Out, "Not logged in.")
			return nil
		}

		// Try to revoke the refresh token server-side
		if creds.RefreshToken != "" {
			body := fmt.Sprintf(`{"refresh_token":"%s"}`, creds.RefreshToken)
			resp, err := apiRequest(cc.ServerURL, creds.AccessToken, "POST", "/v1/auth/token/:revoke", strings.NewReader(body))
			if err == nil {
				_ = resp.Body.Close()
			}
		}

		_ = DeleteCredentials()
		_, _ = fmt.Fprintln(ctx.App.Out, "Logged out.")
		return nil
	}

	return cmd
}

func newAuthStatusCmd() *clix.Command {
	cmd := clix.NewCommand("status")
	cmd.Short = "Show current authentication state"

	cmd.Run = func(ctx *clix.Context) error {
		creds, err := LoadCredentials()
		if err != nil {
			_, _ = fmt.Fprintln(ctx.App.Out, "Not logged in.")
			return nil
		}

		_, _ = fmt.Fprintf(ctx.App.Out, "Server:   %s\n", creds.Server)
		_, _ = fmt.Fprintf(ctx.App.Out, "User:     %s\n", creds.UserID)
		_, _ = fmt.Fprintf(ctx.App.Out, "Provider: %s\n", creds.Provider)

		if creds.ExpiresAt != "" {
			exp, err := time.Parse(time.RFC3339, creds.ExpiresAt)
			if err == nil {
				if time.Now().After(exp) {
					_, _ = fmt.Fprintln(ctx.App.Out, "Token:    expired")
				} else {
					_, _ = fmt.Fprintf(ctx.App.Out, "Token:    valid (expires %s)\n", exp.Format(time.RFC3339))
				}
			}
		}

		return nil
	}

	return cmd
}

func newAuthTokenCmd() *clix.Command {
	cmd := clix.NewCommand("token")
	cmd.Short = "Print current access token"

	cmd.Run = func(ctx *clix.Context) error {
		creds, err := LoadCredentials()
		if err != nil {
			return fmt.Errorf("not logged in")
		}
		_, _ = fmt.Fprint(ctx.App.Out, creds.AccessToken)
		return nil
	}

	return cmd
}

// loginWithBrowser performs the Authorization Code + PKCE flow.
// It starts a local loopback server to receive the callback, opens the browser
// to the authorize endpoint, and exchanges the auth code for tokens.
func loginWithBrowser(ctx *clix.Context, cc *ClientConfig) error {
	// Generate PKCE code verifier and challenge
	verifier, err := auth.GenerateCodeVerifier()
	if err != nil {
		return fmt.Errorf("generate code verifier: %w", err)
	}
	challenge := auth.CodeChallengeS256(verifier)
	state, err := newRandomState()
	if err != nil {
		return fmt.Errorf("generate oauth state: %w", err)
	}

	// Start loopback server on random port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	defer listener.Close()

	port := listener.Addr().(*net.TCPAddr).Port
	redirectURI := fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Channel to receive the auth code
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	// Loopback callback handler
	mux := http.NewServeMux()
	mux.HandleFunc("GET /callback", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("state") != state {
			errCh <- fmt.Errorf("state mismatch")
			http.Error(w, "State mismatch", http.StatusBadRequest)
			return
		}
		if errMsg := q.Get("error"); errMsg != "" {
			errCh <- fmt.Errorf("authorization error: %s", errMsg)
			http.Error(w, errMsg, http.StatusBadRequest)
			return
		}
		code := q.Get("code")
		if code == "" {
			errCh <- fmt.Errorf("no code in callback")
			http.Error(w, "No code", http.StatusBadRequest)
			return
		}
		codeCh <- code
		w.Header().Set("Content-Type", "text/html")
		_, _ = fmt.Fprint(w, `<!DOCTYPE html><html><body><h2>Login successful!</h2><p>You can close this tab.</p><script>window.close()</script></body></html>`)
	})

	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() { _ = server.Serve(listener) }()
	defer func() { _ = server.Shutdown(context.Background()) }()

	// Build authorization URL and open browser
	authURL := fmt.Sprintf("%s/v1/auth/authorize?response_type=code&client_id=cli&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s",
		cc.ServerURL, redirectURI, challenge, state)

	_, _ = fmt.Fprintf(ctx.App.Out, "Opening browser to login...\n")
	_, _ = fmt.Fprintf(ctx.App.Out, "If browser doesn't open, visit: %s\n", authURL)
	openBrowser(authURL)

	// Wait for callback (with timeout)
	timer := time.NewTimer(5 * time.Minute)
	defer timer.Stop()

	var code string
	select {
	case code = <-codeCh:
	case err := <-errCh:
		return err
	case <-timer.C:
		return fmt.Errorf("login timed out after 5 minutes")
	}

	// Exchange code for tokens
	body := fmt.Sprintf(`{"grant_type":"authorization_code","code":"%s","redirect_uri":"%s","code_verifier":"%s","client_id":"cli"}`,
		code, redirectURI, verifier)
	resp, err := apiRequest(cc.ServerURL, "", "POST", "/v1/auth/token", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("token exchange failed: %s", errResp.Error.Message)
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		UserID       string `json:"user_id"`
		Provider     string `json:"provider"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	creds := &Credentials{
		Server:       cc.ServerURL,
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
		UserID:       result.UserID,
		Provider:     result.Provider,
	}
	if err := SaveCredentials(creds); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}

	_, _ = fmt.Fprintf(ctx.App.Out, "Logged in as %s (provider: %s)\n", result.UserID, result.Provider)
	return nil
}

// loginWithDevice performs the Device Authorization Grant (RFC 8628).
// It requests a device code, displays the user code, and polls for approval.
func loginWithDevice(ctx *clix.Context, cc *ClientConfig) error {
	// Request device code
	body := `{"client_id":"cli"}`
	resp, err := apiRequest(cc.ServerURL, "", "POST", "/v1/auth/device", strings.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errResp struct {
			Error struct{ Message string } `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errResp)
		return fmt.Errorf("device authorization failed: %s", errResp.Error.Message)
	}

	var deviceResp struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&deviceResp); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}

	_, _ = fmt.Fprintf(ctx.App.Out, "\nTo authorize this device, visit:\n  %s\n\n", deviceResp.VerificationURI)
	_, _ = fmt.Fprintf(ctx.App.Out, "And enter code: %s\n\n", deviceResp.UserCode)
	_, _ = fmt.Fprintln(ctx.App.Out, "Waiting for authorization...")

	// Try to open browser with pre-filled code
	openBrowser(deviceResp.VerificationURIComplete)

	// Poll for approval
	interval := time.Duration(deviceResp.Interval) * time.Second
	if interval < time.Second {
		interval = 5 * time.Second
	}
	deadline := time.Now().Add(time.Duration(deviceResp.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		time.Sleep(interval)

		pollBody := fmt.Sprintf(`{"device_code":"%s"}`, deviceResp.DeviceCode)
		pollResp, err := apiRequest(cc.ServerURL, "", "POST", "/v1/auth/device/:poll", strings.NewReader(pollBody))
		if err != nil {
			continue
		}

		respBody, _ := io.ReadAll(pollResp.Body)
		_ = pollResp.Body.Close()

		if pollResp.StatusCode == http.StatusOK {
			// Success!
			var result struct {
				AccessToken  string `json:"access_token"`
				RefreshToken string `json:"refresh_token"`
				ExpiresIn    int    `json:"expires_in"`
				UserID       string `json:"user_id"`
				Provider     string `json:"provider"`
			}
			if err := json.Unmarshal(respBody, &result); err != nil {
				return fmt.Errorf("decode token response: %w", err)
			}

			creds := &Credentials{
				Server:       cc.ServerURL,
				AccessToken:  result.AccessToken,
				RefreshToken: result.RefreshToken,
				TokenType:    "Bearer",
				ExpiresAt:    time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339),
				UserID:       result.UserID,
				Provider:     result.Provider,
			}
			if err := SaveCredentials(creds); err != nil {
				return fmt.Errorf("save credentials: %w", err)
			}

			_, _ = fmt.Fprintf(ctx.App.Out, "\nLogged in as %s (provider: %s)\n", result.UserID, result.Provider)
			return nil
		}

		// Check error response
		var errResult struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(respBody, &errResult)

		switch errResult.Error {
		case "authorization_pending":
			continue // keep polling
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return fmt.Errorf("authorization was denied")
		case "expired_token":
			return fmt.Errorf("device code expired")
		default:
			return fmt.Errorf("unexpected error: %s", errResult.Error)
		}
	}

	return fmt.Errorf("device code expired")
}

func newRandomState() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

func openBrowser(url string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("open", url).Start() // #nosec G204 -- url is server-generated verification URI
	case "linux":
		_ = exec.Command("xdg-open", url).Start() // #nosec G204 -- url is server-generated verification URI
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start() // #nosec G204 -- url is server-generated verification URI
	}
}
