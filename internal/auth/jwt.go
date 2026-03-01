package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// JWT implementation using HMAC-SHA256 (HS256) with stdlib only.
// See RFC 7519 - JSON Web Token (JWT)
// https://datatracker.ietf.org/doc/html/rfc7519

var (
	ErrTokenExpired   = errors.New("token expired")
	ErrTokenMalformed = errors.New("malformed token")
	ErrTokenInvalid   = errors.New("invalid token signature")
)

// Claims represents JWT claims.
type Claims struct {
	Subject     string `json:"sub"`
	DisplayName string `json:"name,omitempty"`
	Email       string `json:"email,omitempty"`
	Provider    string `json:"provider,omitempty"`
	TokenType   string `json:"type,omitempty"` // "access" or "refresh"
	IssuedAt    int64  `json:"iat"`
	ExpiresAt   int64  `json:"exp"`
	Issuer      string `json:"iss,omitempty"`
	JWTID       string `json:"jti,omitempty"`
}

// Signer signs and verifies JWTs.
type Signer struct {
	secret []byte
	issuer string
}

// NewSigner creates a JWT signer with the given HMAC secret.
func NewSigner(secret []byte, issuer string) *Signer {
	return &Signer{secret: secret, issuer: issuer}
}

// AccessTokenLifetime is the default lifetime for access tokens.
const AccessTokenLifetime = 1 * time.Hour

// RefreshTokenLifetime is the default lifetime for refresh tokens.
const RefreshTokenLifetime = 30 * 24 * time.Hour

// Sign creates a signed JWT from the given claims.
func (s *Signer) Sign(c Claims) (string, error) {
	if c.Issuer == "" {
		c.Issuer = s.issuer
	}

	header := base64Encode([]byte(`{"alg":"HS256","typ":"JWT"}`))
	payload, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("marshal claims: %w", err)
	}

	payloadEnc := base64Encode(payload)
	signingInput := header + "." + payloadEnc
	sig := s.sign([]byte(signingInput))
	return signingInput + "." + base64Encode(sig), nil
}

// Verify parses and validates a JWT, returning its claims.
func (s *Signer) Verify(token string) (*Claims, error) {
	parts := strings.SplitN(token, ".", 3)
	if len(parts) != 3 {
		return nil, ErrTokenMalformed
	}

	signingInput := parts[0] + "." + parts[1]
	sig, err := base64Decode(parts[2])
	if err != nil {
		return nil, ErrTokenMalformed
	}

	if !s.verify([]byte(signingInput), sig) {
		return nil, ErrTokenInvalid
	}

	payload, err := base64Decode(parts[1])
	if err != nil {
		return nil, ErrTokenMalformed
	}

	var c Claims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil, ErrTokenMalformed
	}

	if time.Now().Unix() > c.ExpiresAt {
		return nil, ErrTokenExpired
	}

	return &c, nil
}

// IssueAccessToken creates an access token for the given identity.
func (s *Signer) IssueAccessToken(id *Identity, jti string) (string, error) {
	now := time.Now()
	return s.Sign(Claims{
		Subject:     id.UserID,
		DisplayName: id.DisplayName,
		Email:       id.Email,
		Provider:    id.Provider,
		TokenType:   "access",
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(AccessTokenLifetime).Unix(),
		JWTID:       jti,
	})
}

// IssueRefreshToken creates a refresh token for the given identity.
func (s *Signer) IssueRefreshToken(id *Identity, jti string) (string, error) {
	now := time.Now()
	return s.Sign(Claims{
		Subject:     id.UserID,
		DisplayName: id.DisplayName,
		Email:       id.Email,
		Provider:    id.Provider,
		TokenType:   "refresh",
		IssuedAt:    now.Unix(),
		ExpiresAt:   now.Add(RefreshTokenLifetime).Unix(),
		JWTID:       jti,
	})
}

func (s *Signer) sign(data []byte) []byte {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(data)
	return mac.Sum(nil)
}

func (s *Signer) verify(data, sig []byte) bool {
	expected := s.sign(data)
	return hmac.Equal(expected, sig)
}

func base64Encode(data []byte) string {
	return base64.RawURLEncoding.EncodeToString(data)
}

func base64Decode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(s)
}

// JWTProvider authenticates requests bearing a valid JWT access token.
type JWTProvider struct {
	signer *Signer
}

// NewJWTProvider creates a provider that validates JWT bearer tokens.
func NewJWTProvider(signer *Signer) *JWTProvider {
	return &JWTProvider{signer: signer}
}

func (p *JWTProvider) Name() string { return "jwt" }

func (p *JWTProvider) Authenticate(r *http.Request) (*Identity, error) {
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, nil // not my request
	}
	token := strings.TrimPrefix(auth, "Bearer ")

	// Only handle JWT-shaped tokens (contain dots)
	if strings.Count(token, ".") != 2 {
		return nil, nil // not a JWT, let next provider try
	}

	claims, err := p.signer.Verify(token)
	if err != nil {
		return nil, fmt.Errorf("invalid jwt: %w", err)
	}

	if claims.TokenType != "access" {
		return nil, errors.New("not an access token")
	}

	return &Identity{
		UserID:      claims.Subject,
		DisplayName: claims.DisplayName,
		Email:       claims.Email,
		Provider:    claims.Provider,
		ExpiresAt:   time.Unix(claims.ExpiresAt, 0),
	}, nil
}
