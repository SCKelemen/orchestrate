package auth

import (
	"net/http/httptest"
	"testing"
)

func TestBearerProvider_ValidToken(t *testing.T) {
	p := NewBearerProvider("my-secret-token")
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer my-secret-token")

	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if id == nil {
		t.Fatal("expected identity, got nil")
	}
	if id.UserID != "bearer" {
		t.Errorf("UserID = %q, want %q", id.UserID, "bearer")
	}
	if id.Provider != "bearer" {
		t.Errorf("Provider = %q, want %q", id.Provider, "bearer")
	}
}

func TestBearerProvider_WrongToken(t *testing.T) {
	p := NewBearerProvider("correct-token")
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")

	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong token")
	}
	if _, ok := err.(*AuthError); !ok {
		t.Errorf("expected *AuthError, got %T", err)
	}
}

func TestBearerProvider_EmptyConfiguredToken(t *testing.T) {
	p := NewBearerProvider("")
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer anything")

	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil (skip), got %+v", id)
	}
}

func TestBearerProvider_NoAuthHeader(t *testing.T) {
	p := NewBearerProvider("token")
	req := httptest.NewRequest("GET", "/api", nil)

	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil for missing header, got %+v", id)
	}
}

func TestBearerProvider_SkipsJWTShapedTokens(t *testing.T) {
	p := NewBearerProvider("token")
	req := httptest.NewRequest("GET", "/api", nil)
	// JWT-shaped token: three dot-separated parts.
	req.Header.Set("Authorization", "Bearer eyJ.eyJ.sig")

	id, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if id != nil {
		t.Fatalf("expected nil for JWT-shaped token, got %+v", id)
	}
}

func TestBearerProvider_ConstantTimeComparison(t *testing.T) {
	// This test verifies that a nearly-correct token still fails.
	p := NewBearerProvider("abcdef1234567890")
	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer abcdef1234567891")

	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for nearly-correct token")
	}
}

// Compile-time interface check.
var _ Provider = (*BearerProvider)(nil)
