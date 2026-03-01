package command

import (
	"testing"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

func TestParseCSVEnv(t *testing.T) {
	t.Setenv("ORCHESTRATE_ALLOWED_IMAGES", "a,b, a ,,b")
	got := parseCSVEnv("ORCHESTRATE_ALLOWED_IMAGES")
	if len(got) != 2 {
		t.Fatalf("len=%d want=2 got=%v", len(got), got)
	}
	if got[0] != "a" || got[1] != "b" {
		t.Fatalf("got=%v want=[a b]", got)
	}
}

func TestParseBoolEnv(t *testing.T) {
	t.Setenv("BOOL_ENV", "true")
	if !parseBoolEnv("BOOL_ENV") {
		t.Fatal("expected true")
	}

	t.Setenv("BOOL_ENV", "1")
	if !parseBoolEnv("BOOL_ENV") {
		t.Fatal("expected true")
	}

	t.Setenv("BOOL_ENV", "false")
	if parseBoolEnv("BOOL_ENV") {
		t.Fatal("expected false")
	}
}

func TestParseSandboxNetworkMode(t *testing.T) {
	mode, err := parseSandboxNetworkMode("")
	if err != nil {
		t.Fatalf("empty mode: %v", err)
	}
	if mode != sandbox.NetworkModeDefault {
		t.Fatalf("mode=%q want=%q", mode, sandbox.NetworkModeDefault)
	}

	mode, err = parseSandboxNetworkMode("none")
	if err != nil {
		t.Fatalf("none mode: %v", err)
	}
	if mode != sandbox.NetworkModeNone {
		t.Fatalf("mode=%q want=%q", mode, sandbox.NetworkModeNone)
	}

	if _, err := parseSandboxNetworkMode("bridge"); err == nil {
		t.Fatal("expected error for unsupported mode")
	}
}
