package command

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndLoadCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	creds := &Credentials{
		Server:       "http://localhost:8080",
		AccessToken:  "access-token",
		RefreshToken: "refresh-token",
		TokenType:    "Bearer",
		ExpiresAt:    time.Now().Add(time.Hour).Format(time.RFC3339),
		UserID:       "u1",
		Provider:     "github",
	}
	if err := SaveCredentials(creds); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	loaded, err := LoadCredentials()
	if err != nil {
		t.Fatalf("load credentials: %v", err)
	}
	if loaded.AccessToken != creds.AccessToken || loaded.UserID != creds.UserID {
		t.Fatalf("loaded creds mismatch: %+v", loaded)
	}

	info, err := os.Stat(credentialsPath())
	if err != nil {
		t.Fatalf("stat credentials: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("permissions=%04o want=0600", got)
	}
}

func TestResolveTokenPriority(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SaveCredentials(&Credentials{
		Server:      "http://localhost:8080",
		AccessToken: "stored-token",
		ExpiresAt:   time.Now().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}

	cc := &ClientConfig{
		ServerURL: "http://localhost:8080",
		Token:     "explicit-token",
	}
	if got := cc.ResolveToken(); got != "explicit-token" {
		t.Fatalf("token=%q want explicit-token", got)
	}

	cc.Token = ""
	if got := cc.ResolveToken(); got != "stored-token" {
		t.Fatalf("token=%q want stored-token", got)
	}
}

func TestDeleteCredentials(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	if err := SaveCredentials(&Credentials{AccessToken: "t"}); err != nil {
		t.Fatalf("save credentials: %v", err)
	}
	if err := DeleteCredentials(); err != nil {
		t.Fatalf("delete credentials: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "orchestrate", "credentials.json")); !os.IsNotExist(err) {
		t.Fatalf("expected credentials file removed, stat err=%v", err)
	}
}
