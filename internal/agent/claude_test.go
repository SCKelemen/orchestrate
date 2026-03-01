package agent

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

type fakeSandbox struct {
	lastCmd []string
	result  *sandbox.ExecResult
	err     error
}

func (f *fakeSandbox) Create(ctx context.Context, opts sandbox.CreateOpts) (*sandbox.Workspace, error) {
	return &sandbox.Workspace{}, nil
}

func (f *fakeSandbox) Exec(ctx context.Context, ws *sandbox.Workspace, cmd []string) (*sandbox.ExecResult, error) {
	f.lastCmd = append([]string{}, cmd...)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeSandbox) Destroy(ctx context.Context, ws *sandbox.Workspace) error { return nil }

func TestClaudeRunBuildsCommandAndReturnsResult(t *testing.T) {
	t.Parallel()

	sb := &fakeSandbox{
		result: &sandbox.ExecResult{
			ExitCode: 0,
			Stdout:   `{"ok":true}`,
			Stderr:   "",
		},
	}
	c := NewClaude(sb)

	res, err := c.Run(context.Background(), &sandbox.Workspace{}, "hello", RunOpts{
		Model:        "claude-3-7-sonnet",
		MaxTurns:     5,
		MaxBudgetUSD: 1.25,
		AllowedTools: []string{"bash", "read_file"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit code=%d want=0", res.ExitCode)
	}

	wantPrefix := []string{
		"claude",
		"-p", "hello",
		"--output-format", "json",
		"--dangerously-skip-permissions",
		"--model", "claude-3-7-sonnet",
		"--max-turns", "5",
		"--max-budget-usd", "1.25",
		"--allowedTools", "bash,read_file",
	}
	if !reflect.DeepEqual(sb.lastCmd, wantPrefix) {
		t.Fatalf("command mismatch\ngot:  %#v\nwant: %#v", sb.lastCmd, wantPrefix)
	}
}

func TestClaudeRunWritesLogFile(t *testing.T) {
	t.Parallel()

	sb := &fakeSandbox{
		result: &sandbox.ExecResult{
			ExitCode: 0,
			Stdout:   "stdout-line",
			Stderr:   "stderr-line",
		},
	}
	c := NewClaude(sb)
	logPath := filepath.Join(t.TempDir(), "run.log")

	if _, err := c.Run(context.Background(), &sandbox.Workspace{}, "hello", RunOpts{LogPath: logPath}); err != nil {
		t.Fatalf("run: %v", err)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("read log file: %v", err)
	}
	if got := string(data); got != "stdout-line\n---STDERR---\nstderr-line" {
		t.Fatalf("unexpected log contents: %q", got)
	}

	info, err := os.Stat(logPath)
	if err != nil {
		t.Fatalf("stat log file: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("mode=%04o want=0600", got)
	}
}
