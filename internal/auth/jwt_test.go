package auth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testSigner() *Signer {
	return NewSigner([]byte("test-secret-32-bytes-long-xxxxx"), "test-issuer")
}

func TestSignAndVerify(t *testing.T) {
	s := testSigner()
	claims := Claims{
		Subject:   "user-1",
		TokenType: "access",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	token, err := s.Sign(claims)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	// Token should have 3 dot-separated parts.
	if parts := strings.Count(token, "."); parts != 2 {
		t.Fatalf("expected 2 dots in JWT, got %d", parts)
	}

	got, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if got.Subject != "user-1" {
		t.Errorf("Subject = %q, want %q", got.Subject, "user-1")
	}
	if got.Issuer != "test-issuer" {
		t.Errorf("Issuer = %q, want %q", got.Issuer, "test-issuer")
	}
}

func TestVerify_ExpiredToken(t *testing.T) {
	s := testSigner()
	claims := Claims{
		Subject:   "user-1",
		TokenType: "access",
		IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	}

	token, err := s.Sign(claims)
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, err = s.Verify(token)
	if err != ErrTokenExpired {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestVerify_WrongSecret(t *testing.T) {
	s1 := NewSigner([]byte("secret-one-xxxxxxxxxxxxxxx-xxxx"), "iss")
	s2 := NewSigner([]byte("secret-two-xxxxxxxxxxxxxxx-xxxx"), "iss")

	token, err := s1.Sign(Claims{
		Subject:   "user-1",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})
	if err != nil {
		t.Fatalf("Sign: %v", err)
	}

	_, err = s2.Verify(token)
	if err != ErrTokenInvalid {
		t.Errorf("expected ErrTokenInvalid, got %v", err)
	}
}

func TestVerify_Malformed(t *testing.T) {
	s := testSigner()

	cases := []struct {
		name  string
		token string
	}{
		{"empty", ""},
		{"no dots", "nodots"},
		{"one dot", "one.dot"},
		{"bad base64 sig", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ4In0.!!!"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Verify(tc.token)
			if err != ErrTokenMalformed {
				t.Errorf("expected ErrTokenMalformed, got %v", err)
			}
		})
	}
}

func TestIssueAccessToken(t *testing.T) {
	s := testSigner()
	id := &Identity{
		UserID:      "u1",
		DisplayName: "User One",
		Email:       "u1@example.com",
		Provider:    "github",
	}

	token, err := s.IssueAccessToken(id, "jti-123")
	if err != nil {
		t.Fatalf("IssueAccessToken: %v", err)
	}

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if claims.Subject != "u1" {
		t.Errorf("Subject = %q, want %q", claims.Subject, "u1")
	}
	if claims.TokenType != "access" {
		t.Errorf("TokenType = %q, want %q", claims.TokenType, "access")
	}
	if claims.JWTID != "jti-123" {
		t.Errorf("JWTID = %q, want %q", claims.JWTID, "jti-123")
	}
	if claims.Email != "u1@example.com" {
		t.Errorf("Email = %q, want %q", claims.Email, "u1@example.com")
	}

	// Expiry should be roughly 1 hour from now.
	expiry := time.Unix(claims.ExpiresAt, 0)
	if d := time.Until(expiry); d < 59*time.Minute || d > 61*time.Minute {
		t.Errorf("access token expiry = %v from now, want ~1h", d)
	}
}

func TestIssueRefreshToken(t *testing.T) {
	s := testSigner()
	id := &Identity{UserID: "u1", Provider: "github"}

	token, err := s.IssueRefreshToken(id, "jti-456")
	if err != nil {
		t.Fatalf("IssueRefreshToken: %v", err)
	}

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}

	if claims.TokenType != "refresh" {
		t.Errorf("TokenType = %q, want %q", claims.TokenType, "refresh")
	}

	// Expiry should be roughly 30 days from now.
	expiry := time.Unix(claims.ExpiresAt, 0)
	expected := 30 * 24 * time.Hour
	if d := time.Until(expiry); d < expected-time.Minute || d > expected+time.Minute {
		t.Errorf("refresh token expiry = %v from now, want ~%v", d, expected)
	}
}

func TestJWTProvider_ValidAccessToken(t *testing.T) {
	s := testSigner()
	p := NewJWTProvider(s)
	id := &Identity{UserID: "u1", Provider: "jwt"}

	token, _ := s.IssueAccessToken(id, "jti-1")

	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	got, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got == nil {
		t.Fatal("expected identity, got nil")
	}
	if got.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "u1")
	}
}

func TestJWTProvider_RefreshTokenRejected(t *testing.T) {
	s := testSigner()
	p := NewJWTProvider(s)
	id := &Identity{UserID: "u1", Provider: "jwt"}

	token, _ := s.IssueRefreshToken(id, "jti-2")

	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for refresh token, got nil")
	}
}

func TestJWTProvider_NonJWTSkipped(t *testing.T) {
	s := testSigner()
	p := NewJWTProvider(s)

	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer some-opaque-token")

	got, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil identity for non-JWT, got %+v", got)
	}
}

func TestJWTProvider_NoAuthHeader(t *testing.T) {
	s := testSigner()
	p := NewJWTProvider(s)

	req := httptest.NewRequest("GET", "/api", nil)

	got, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing auth header, got %+v", got)
	}
}

func TestJWTProvider_ExpiredToken(t *testing.T) {
	s := testSigner()
	p := NewJWTProvider(s)

	token, _ := s.Sign(Claims{
		Subject:   "u1",
		TokenType: "access",
		IssuedAt:  time.Now().Add(-2 * time.Hour).Unix(),
		ExpiresAt: time.Now().Add(-1 * time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for expired token")
	}
}

func TestSignSetsDefaultIssuer(t *testing.T) {
	s := NewSigner([]byte("secret-xxxxxxxxxxxxxxxxxxxxxxxxx"), "my-issuer")
	token, _ := s.Sign(Claims{
		Subject:   "u",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Issuer != "my-issuer" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "my-issuer")
	}
}

func TestSignPreservesExplicitIssuer(t *testing.T) {
	s := NewSigner([]byte("secret-xxxxxxxxxxxxxxxxxxxxxxxxx"), "default")
	token, _ := s.Sign(Claims{
		Subject:   "u",
		Issuer:    "custom",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	claims, err := s.Verify(token)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if claims.Issuer != "custom" {
		t.Errorf("Issuer = %q, want %q", claims.Issuer, "custom")
	}
}

func TestJWTProvider_BasicAuthSkipped(t *testing.T) {
	s := testSigner()
	p := NewJWTProvider(s)

	req := httptest.NewRequest("GET", "/api", nil)
	req.SetBasicAuth("user", "pass")

	got, err := p.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for Basic auth, got %+v", got)
	}
}

func TestJWTProvider_WrongSecretReturnsError(t *testing.T) {
	s1 := NewSigner([]byte("secret-one-xxxxxxxxxxxxxxx-xxxx"), "iss")
	s2 := NewSigner([]byte("secret-two-xxxxxxxxxxxxxxx-xxxx"), "iss")
	p := NewJWTProvider(s2)

	token, _ := s1.Sign(Claims{
		Subject:   "u1",
		TokenType: "access",
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	})

	req := httptest.NewRequest("GET", "/api", nil)
	req.Header.Set("Authorization", "Bearer "+token)

	_, err := p.Authenticate(req)
	if err == nil {
		t.Fatal("expected error for wrong secret")
	}
}

func BenchmarkSignVerify(b *testing.B) {
	s := testSigner()
	claims := Claims{
		Subject:   "user-bench",
		TokenType: "access",
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: time.Now().Add(time.Hour).Unix(),
	}

	for b.Loop() {
		token, err := s.Sign(claims)
		if err != nil {
			b.Fatal(err)
		}
		if _, err := s.Verify(token); err != nil {
			b.Fatal(err)
		}
	}
}

// Ensure NewJWTProvider satisfies the Provider interface at compile time.
var _ Provider = (*JWTProvider)(nil)

// Helper to make a request with Bearer token for benchmarks.
func reqWithBearer(token string) *http.Request {
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}
