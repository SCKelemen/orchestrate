package auth

import (
	"context"
	"testing"
)

func TestWithIdentityAndFromContext(t *testing.T) {
	id := &Identity{
		UserID:   "user-1",
		Provider: "test",
	}

	ctx := WithIdentity(context.Background(), id)
	got := FromContext(ctx)

	if got == nil {
		t.Fatal("expected identity, got nil")
	}
	if got.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", got.UserID, "user-1")
	}
	if got.Provider != "test" {
		t.Errorf("Provider = %q, want %q", got.Provider, "test")
	}
}

func TestFromContext_Empty(t *testing.T) {
	got := FromContext(context.Background())
	if got != nil {
		t.Errorf("expected nil identity from empty context, got %+v", got)
	}
}

func TestFromContext_NilContext(t *testing.T) {
	// Ensure nil value in context returns nil identity gracefully.
	ctx := context.WithValue(context.Background(), contextKey{}, (*Identity)(nil))
	got := FromContext(ctx)
	if got != nil {
		t.Errorf("expected nil for nil *Identity in context, got %+v", got)
	}
}
