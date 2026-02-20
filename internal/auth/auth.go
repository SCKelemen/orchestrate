package auth

import (
	"context"
	"net/http"
	"time"
)

// Identity represents an authenticated user.
type Identity struct {
	UserID      string
	DisplayName string
	Email       string
	Provider    string // "bearer", "jwt", "github", "google", "webauthn", "device", "ciba"
	ExpiresAt   time.Time
}

// Provider authenticates an HTTP request and returns an Identity.
// Returns (nil, nil) if the provider does not recognize the request (try next).
// Returns (nil, err) if the provider recognized the request but it was invalid (401).
type Provider interface {
	Name() string
	Authenticate(r *http.Request) (*Identity, error)
}

type contextKey struct{}

// WithIdentity returns a new context with the given identity attached.
func WithIdentity(ctx context.Context, id *Identity) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// FromContext extracts the identity from the context, or nil if not present.
func FromContext(ctx context.Context) *Identity {
	id, _ := ctx.Value(contextKey{}).(*Identity)
	return id
}
