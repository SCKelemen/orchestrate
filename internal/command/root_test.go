package command

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirFromEnv(t *testing.T) {
	t.Setenv("ORCHESTRATE_DATA_DIR", "/custom/path")
	got := dataDir()
	if got != "/custom/path" {
		t.Errorf("dataDir() = %q, want /custom/path", got)
	}
}

func TestDataDirDefault(t *testing.T) {
	t.Setenv("ORCHESTRATE_DATA_DIR", "")
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".local", "share", "orchestrate")
	got := dataDir()
	if got != want {
		t.Errorf("dataDir() = %q, want %q", got, want)
	}
}
