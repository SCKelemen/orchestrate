package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// GitHubExchange validates a GitHub personal access token by calling
// api.github.com/user and returns the authenticated identity.
type GitHubExchange struct {
	client *http.Client
}

// NewGitHubExchange creates a new GitHub token exchange handler.
func NewGitHubExchange() *GitHubExchange {
	return &GitHubExchange{
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

// GitHubUser represents the subset of the GitHub user API response we need.
type GitHubUser struct {
	ID    int    `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Exchange validates a GitHub token and returns the GitHub user info.
// See https://docs.github.com/en/rest/users/users#get-the-authenticated-user
func (g *GitHubExchange) Exchange(token string) (*GitHubUser, error) {
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github api returned %d: %s", resp.StatusCode, body)
	}

	var user GitHubUser
	if err := json.NewDecoder(resp.Body).Decode(&user); err != nil {
		return nil, fmt.Errorf("decode github user: %w", err)
	}
	return &user, nil
}
