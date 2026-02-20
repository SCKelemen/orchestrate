package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerProvider authenticates requests using a static bearer token.
// This provides backward compatibility with the original single-token auth.
type BearerProvider struct {
	token string
}

// NewBearerProvider creates a static bearer token provider.
// If token is empty, this provider always returns (nil, nil) (skips).
func NewBearerProvider(token string) *BearerProvider {
	return &BearerProvider{token: token}
}

func (p *BearerProvider) Name() string { return "bearer" }

func (p *BearerProvider) Authenticate(r *http.Request) (*Identity, error) {
	if p.token == "" {
		return nil, nil // no static token configured, skip
	}

	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") {
		return nil, nil // not a bearer request
	}

	token := strings.TrimPrefix(auth, "Bearer ")

	// Skip JWT-shaped tokens (let JWTProvider handle those)
	if strings.Count(token, ".") == 2 {
		return nil, nil
	}

	if subtle.ConstantTimeCompare([]byte(token), []byte(p.token)) != 1 {
		return nil, errInvalidToken
	}

	return &Identity{
		UserID:   "bearer",
		Provider: "bearer",
	}, nil
}

var errInvalidToken = &AuthError{Message: "invalid bearer token"}

// AuthError represents an authentication error.
type AuthError struct {
	Message string
}

func (e *AuthError) Error() string { return e.Message }
