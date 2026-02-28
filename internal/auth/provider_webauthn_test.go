package auth

import (
	"testing"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
)

func TestWebAuthnSessionStore_SaveAndGet(t *testing.T) {
	store := NewWebAuthnSessionStore()
	sd := &webauthn.SessionData{
		Challenge: "test-challenge",
		UserID:    []byte("user-1"),
	}

	store.Save("key-1", sd)
	got, ok := store.Get("key-1")
	if !ok {
		t.Fatal("expected to find session")
	}
	if got.Challenge != "test-challenge" {
		t.Errorf("Challenge = %q, want %q", got.Challenge, "test-challenge")
	}
}

func TestWebAuthnSessionStore_GetRemovesEntry(t *testing.T) {
	store := NewWebAuthnSessionStore()
	store.Save("key-1", &webauthn.SessionData{Challenge: "c"})

	// First Get should succeed.
	_, ok := store.Get("key-1")
	if !ok {
		t.Fatal("first Get should succeed")
	}

	// Second Get should fail (entry removed).
	_, ok = store.Get("key-1")
	if ok {
		t.Error("second Get should fail (entry was consumed)")
	}
}

func TestWebAuthnSessionStore_MissingKey(t *testing.T) {
	store := NewWebAuthnSessionStore()
	_, ok := store.Get("nonexistent")
	if ok {
		t.Error("expected false for missing key")
	}
}

func TestWebAuthnSessionStore_Expired(t *testing.T) {
	store := NewWebAuthnSessionStore()
	sd := &webauthn.SessionData{Challenge: "c"}

	store.Save("key-1", sd)

	// Manually expire the entry.
	store.mu.Lock()
	store.sessions["key-1"].expiresAt = time.Now().Add(-1 * time.Second)
	store.mu.Unlock()

	_, ok := store.Get("key-1")
	if ok {
		t.Error("expected false for expired session")
	}
}

func TestMarshalUnmarshalSessionData(t *testing.T) {
	sd := &webauthn.SessionData{
		Challenge: "test-challenge",
		UserID:    []byte("user-1"),
	}

	data, err := MarshalSessionData(sd)
	if err != nil {
		t.Fatalf("MarshalSessionData: %v", err)
	}

	got, err := UnmarshalSessionData(data)
	if err != nil {
		t.Fatalf("UnmarshalSessionData: %v", err)
	}
	if got.Challenge != "test-challenge" {
		t.Errorf("Challenge = %q, want %q", got.Challenge, "test-challenge")
	}
}

func TestUnmarshalSessionData_Invalid(t *testing.T) {
	_, err := UnmarshalSessionData([]byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestMarshalUnmarshalCredential(t *testing.T) {
	cred := &webauthn.Credential{
		ID: []byte("cred-1"),
	}

	data, err := MarshalCredential(cred)
	if err != nil {
		t.Fatalf("MarshalCredential: %v", err)
	}

	got, err := UnmarshalCredential(data)
	if err != nil {
		t.Fatalf("UnmarshalCredential: %v", err)
	}
	if string(got.ID) != "cred-1" {
		t.Errorf("ID = %q, want %q", got.ID, "cred-1")
	}
}

func TestUnmarshalCredential_Invalid(t *testing.T) {
	_, err := UnmarshalCredential([]byte("{bad"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestWebAuthnUser_Interface(t *testing.T) {
	u := &WebAuthnUser{
		ID:          []byte("u1"),
		Name:        "user@example.com",
		DisplayName: "Test User",
		Credentials: []webauthn.Credential{{ID: []byte("c1")}},
	}

	if string(u.WebAuthnID()) != "u1" {
		t.Errorf("WebAuthnID = %q, want %q", u.WebAuthnID(), "u1")
	}
	if u.WebAuthnName() != "user@example.com" {
		t.Errorf("WebAuthnName = %q, want %q", u.WebAuthnName(), "user@example.com")
	}
	if u.WebAuthnDisplayName() != "Test User" {
		t.Errorf("WebAuthnDisplayName = %q, want %q", u.WebAuthnDisplayName(), "Test User")
	}
	if len(u.WebAuthnCredentials()) != 1 {
		t.Errorf("len(WebAuthnCredentials) = %d, want 1", len(u.WebAuthnCredentials()))
	}
}
