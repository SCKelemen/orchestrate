package agent

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

type fakeSandboxExec struct {
	lastCmd []string
	result  *sandbox.ExecResult
	err     error
}

func (f *fakeSandboxExec) Create(ctx context.Context, opts sandbox.CreateOpts) (*sandbox.Workspace, error) {
	return &sandbox.Workspace{}, nil
}

func (f *fakeSandboxExec) Exec(ctx context.Context, ws *sandbox.Workspace, cmd []string) (*sandbox.ExecResult, error) {
	f.lastCmd = append([]string{}, cmd...)
	if f.err != nil {
		return nil, f.err
	}
	return f.result, nil
}

func (f *fakeSandboxExec) Destroy(ctx context.Context, ws *sandbox.Workspace) error { return nil }

func TestCodexRunBuildsCommand(t *testing.T) {
	t.Parallel()

	sb := &fakeSandboxExec{
		result: &sandbox.ExecResult{ExitCode: 0, Stdout: "ok", Stderr: ""},
	}
	c := NewCodex(sb)

	res, err := c.Run(context.Background(), &sandbox.Workspace{}, "fix bug", RunOpts{Model: "o3"})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("exit=%d want=0", res.ExitCode)
	}

	want := []string{
		"codex", "exec",
		"--json",
		"--skip-git-repo-check",
		"--dangerously-bypass-approvals-and-sandbox",
		"--model", "o3",
		"fix bug",
	}
	if !reflect.DeepEqual(sb.lastCmd, want) {
		t.Fatalf("cmd mismatch\ngot:  %#v\nwant: %#v", sb.lastCmd, want)
	}
}

func TestCodexRunPropagatesExecError(t *testing.T) {
	t.Parallel()

	sb := &fakeSandboxExec{err: errors.New("exec failed")}
	c := NewCodex(sb)

	if _, err := c.Run(context.Background(), &sandbox.Workspace{}, "prompt", RunOpts{}); err == nil {
		t.Fatal("expected error")
	}
}

func TestNormalizeBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		in      string
		want    string
		wantErr bool
	}{
		{in: "", want: BackendClaude},
		{in: "claude", want: BackendClaude},
		{in: "anthropic", want: BackendClaude},
		{in: "codex", want: BackendCodex},
		{in: "openai", want: BackendCodex},
		{in: "unknown", wantErr: true},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			got, err := NormalizeBackend(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got=%q want=%q", got, tc.want)
			}
		})
	}
}
