package auth

import (
	"fmt"
	"net/http"
	"strings"
)

// Middleware chains multiple auth providers and injects Identity into context.
type Middleware struct {
	providers   []Provider
	publicPaths map[string]bool
}

// NewMiddleware creates auth middleware with the given provider chain.
func NewMiddleware(providers ...Provider) *Middleware {
	return &Middleware{
		providers: providers,
		publicPaths: map[string]bool{
			"/healthz": true,
		},
	}
}

// AddPublicPath marks a path prefix as publicly accessible (no auth required).
func (m *Middleware) AddPublicPath(path string) {
	m.publicPaths[path] = true
}

// Wrap wraps a handler with authentication.
func (m *Middleware) Wrap(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check public paths
		for path := range m.publicPaths {
			if r.URL.Path == path || strings.HasPrefix(r.URL.Path, path+"/") {
				next(w, r)
				return
			}
		}

		// No providers configured = open access (backward compat with no token)
		if len(m.providers) == 0 {
			next(w, r)
			return
		}

		// Try each provider in order.
		id, err := m.Authenticate(r)
		if err != nil {
			writeAuthError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		if id != nil {
			// Authenticated; inject identity into context
			ctx := WithIdentity(r.Context(), id)
			next(w, r.WithContext(ctx))
			return
		}

		// No provider matched
		writeAuthError(w, http.StatusUnauthorized, "unauthorized")
	}
}

// Authenticate runs the provider chain and returns the first successful identity.
// Returns (nil, nil) when no provider recognizes the request.
func (m *Middleware) Authenticate(r *http.Request) (*Identity, error) {
	for _, p := range m.providers {
		id, err := p.Authenticate(r)
		if err != nil {
			// Provider recognized the request but it was invalid.
			return nil, err
		}
		if id != nil {
			return id, nil
		}
		// (nil, nil) = not my request, try next
	}
	return nil, nil
}

func writeAuthError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = fmt.Fprintf(w, `{"error":{"code":%d,"message":"%s"}}`, code, msg)
}
