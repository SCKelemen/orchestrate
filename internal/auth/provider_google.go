package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GoogleExchange validates a Google access token by calling the userinfo endpoint.
type GoogleExchange struct {
	client *http.Client
}

// NewGoogleExchange creates a new Google token exchange handler.
func NewGoogleExchange() *GoogleExchange {
	return &GoogleExchange{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// GoogleUser represents the subset of the Google userinfo response we need.
type GoogleUser struct {
	Sub     string `json:"sub"`
	Name    string `json:"name"`
	Email   string `json:"email"`
	Picture string `json:"picture"`
}

// Exchange validates a Google access token and returns the Google user info.
// See https://developers.google.com/identity/protocols/oauth2/openid-connect#obtainuserinfo
func (g *GoogleExchange) Exchange(token string) (*GoogleUser, error) {
	req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("google api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("google api returned %d: %s", resp.StatusCode, body)
	}

	var user GoogleUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode google user: %w", err)
	}
	return &user, nil
}
