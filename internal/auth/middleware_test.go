package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockProvider is a test helper that returns preconfigured results.
type mockProvider struct {
	name string
	id   *Identity
	err  error
}

func (m *mockProvider) Name() string { return m.name }
func (m *mockProvider) Authenticate(r *http.Request) (*Identity, error) {
	return m.id, m.err
}

func TestMiddleware_PublicPath(t *testing.T) {
	mw := NewMiddleware(&mockProvider{
		name: "always-fail",
		err:  errors.New("should not be called"),
	})

	var called bool
	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	// /healthz is public by default.
	req := httptest.NewRequest("GET", "/healthz", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("handler was not called for public path")
	}
	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMiddleware_PublicPathPrefix(t *testing.T) {
	mw := NewMiddleware(&mockProvider{name: "fail", err: errors.New("fail")})
	mw.AddPublicPath("/v1/auth")

	var called bool
	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/v1/auth/device", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("handler was not called for public path prefix")
	}
}

func TestMiddleware_NoProviders_OpenAccess(t *testing.T) {
	mw := NewMiddleware() // no providers

	var called bool
	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		called = true
	})

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if !called {
		t.Error("handler was not called with no providers (open access)")
	}
}

func TestMiddleware_SuccessfulAuth(t *testing.T) {
	id := &Identity{UserID: "u1", Provider: "test"}
	mw := NewMiddleware(&mockProvider{name: "test", id: id})

	var gotID *Identity
	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		gotID = FromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if gotID == nil {
		t.Fatal("expected identity in context")
	}
	if gotID.UserID != "u1" {
		t.Errorf("UserID = %q, want %q", gotID.UserID, "u1")
	}
}

func TestMiddleware_ProviderError_Returns401(t *testing.T) {
	mw := NewMiddleware(&mockProvider{
		name: "fail",
		err:  errors.New("bad token"),
	})

	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called on auth failure")
	})

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_NoMatch_Returns401(t *testing.T) {
	// Provider returns (nil, nil) = "not my request".
	mw := NewMiddleware(&mockProvider{name: "skip"})

	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called when no provider matches")
	})

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_ProviderChainOrder(t *testing.T) {
	// First provider skips, second provider authenticates.
	id := &Identity{UserID: "u2", Provider: "second"}
	mw := NewMiddleware(
		&mockProvider{name: "skip"},
		&mockProvider{name: "match", id: id},
	)

	var gotID *Identity
	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		gotID = FromContext(r.Context())
	})

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if gotID == nil {
		t.Fatal("expected identity from second provider")
	}
	if gotID.Provider != "second" {
		t.Errorf("Provider = %q, want %q", gotID.Provider, "second")
	}
}

func TestMiddleware_FirstProviderError_StopsChain(t *testing.T) {
	// First provider returns an error; second provider should not be tried.
	mw := NewMiddleware(
		&mockProvider{name: "fail", err: errors.New("invalid")},
		&mockProvider{name: "should-not-run", id: &Identity{UserID: "u3"}},
	)

	handler := mw.Wrap(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called on auth failure")
	})

	req := httptest.NewRequest("GET", "/api/tasks", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

func TestMiddleware_Authenticate_Standalone(t *testing.T) {
	id := &Identity{UserID: "u1", Provider: "test"}
	mw := NewMiddleware(&mockProvider{name: "test", id: id})

	req := httptest.NewRequest("GET", "/api", nil)
	got, err := mw.Authenticate(req)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if got == nil || got.UserID != "u1" {
		t.Errorf("got %+v, want identity with UserID=u1", got)
	}
}

func TestMiddleware_Authenticate_NoMatch(t *testing.T) {
	mw := NewMiddleware(&mockProvider{name: "skip"})

	req := httptest.NewRequest("GET", "/api", nil)
	got, err := mw.Authenticate(req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestWriteAuthError_JSON(t *testing.T) {
	w := httptest.NewRecorder()
	writeAuthError(w, http.StatusUnauthorized, "unauthorized")

	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
	body := w.Body.String()
	if body != `{"error":{"code":401,"message":"unauthorized"}}` {
		t.Errorf("body = %q", body)
	}
}
