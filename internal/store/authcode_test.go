package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestConsumeAuthCodeOneTime(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "store.db")
	s, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	ctx := context.Background()
	user, err := s.CreateUser(ctx, "u1", CreateUserParams{
		DisplayName: "User",
		Email:       "u1@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if err := s.CreateAuthCode(ctx, "code-1", CreateAuthCodeParams{
		UserID:              user.ID,
		ClientID:            "client-1",
		RedirectURI:         "http://localhost/callback",
		CodeChallenge:       "challenge",
		CodeChallengeMethod: "S256",
		ExpiresAt:           time.Now().Add(10 * time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("create auth code: %v", err)
	}

	if err := s.ConsumeAuthCode(ctx, "code-1"); err != nil {
		t.Fatalf("first consume should succeed: %v", err)
	}
	if err := s.ConsumeAuthCode(ctx, "code-1"); err == nil {
		t.Fatalf("second consume should fail")
	}
}
