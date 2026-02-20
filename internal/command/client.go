package command

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/SCKelemen/clix/v2"
)

// ClientConfig consolidates the --server and --token flags used by all client commands.
type ClientConfig struct {
	ServerURL string
	Token     string
}

// RegisterFlags adds --server and --token flags to a command.
func (c *ClientConfig) RegisterFlags(cmd *clix.Command) {
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "server", Short: "s", EnvVar: "ORCHESTRATE_SERVER"},
		Default:     "http://localhost:8080",
		Value:       &c.ServerURL,
	})
	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "token", Short: "t", EnvVar: "ORCHESTRATE_TOKEN"},
		Value:       &c.Token,
	})
}

// Credentials represents stored authentication credentials.
type Credentials struct {
	Server       string `json:"server"`
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	UserID       string `json:"user_id,omitempty"`
	Provider     string `json:"provider,omitempty"`
}

// credentialsPath returns the path to the credentials file.
func credentialsPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "orchestrate", "credentials.json")
}

// LoadCredentials reads credentials from disk.
func LoadCredentials() (*Credentials, error) {
	data, err := os.ReadFile(credentialsPath())
	if err != nil {
		return nil, err
	}
	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, err
	}
	return &creds, nil
}

// SaveCredentials writes credentials to disk with restricted permissions.
func SaveCredentials(creds *Credentials) error {
	path := credentialsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(creds, "", "    ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
}

// DeleteCredentials removes stored credentials.
func DeleteCredentials() error {
	return os.Remove(credentialsPath())
}

// ResolveToken returns the token to use for API requests.
// Priority: explicit --token flag > stored credentials > empty
func (c *ClientConfig) ResolveToken() string {
	if c.Token != "" {
		return c.Token
	}
	creds, err := LoadCredentials()
	if err != nil {
		return ""
	}
	// Check if access token is expired and we have a refresh token
	if creds.ExpiresAt != "" && creds.RefreshToken != "" {
		expiresAt, err := time.Parse(time.RFC3339, creds.ExpiresAt)
		if err == nil && time.Now().After(expiresAt) {
			// Try to refresh
			if refreshed := refreshAccessToken(c.ServerURL, creds); refreshed != nil {
				return refreshed.AccessToken
			}
		}
	}
	return creds.AccessToken
}

// APIRequest sends an authenticated HTTP request to the server.
func (c *ClientConfig) APIRequest(method, path string, body io.Reader) (*http.Response, error) {
	token := c.ResolveToken()
	return apiRequest(c.ServerURL, token, method, path, body)
}

func refreshAccessToken(serverURL string, creds *Credentials) *Credentials {
	body := fmt.Sprintf(`{"grant_type":"refresh_token","refresh_token":"%s"}`, creds.RefreshToken)
	resp, err := apiRequest(serverURL, "", "POST", "/v1/auth/token", strings.NewReader(body))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil
	}

	creds.AccessToken = result.AccessToken
	if result.RefreshToken != "" {
		creds.RefreshToken = result.RefreshToken
	}
	creds.ExpiresAt = time.Now().Add(time.Duration(result.ExpiresIn) * time.Second).Format(time.RFC3339)
	SaveCredentials(creds)
	return creds
}
