package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"sync/atomic"
)

// Docker implements Sandbox using Docker containers via os/exec.
type Docker struct {
	dataDir string
}

// NewDocker creates a Docker sandbox manager.
func NewDocker(dataDir string) *Docker {
	return &Docker{dataDir: dataDir}
}

func (d *Docker) Create(ctx context.Context, opts CreateOpts) (*Workspace, error) {
	ws := &Workspace{
		ID:      fmt.Sprintf("orch-%d", uniqueCounter()),
		Branch:  opts.Branch,
		RepoURL: opts.RepoURL,
	}

	args := []string{
		"create",
		"--name", ws.ID,
		"--label", "orchestrate=true",
		"-w", "/home/agent/workspace",
	}

	for k, v := range opts.EnvVars {
		args = append(args, "-e", k+"="+v)
	}

	args = append(args, opts.Image, "sleep", "infinity")

	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker create: %w: %s", err, out.String())
	}
	ws.ContainerID = strings.TrimSpace(out.String())

	// Start the container
	if err := dockerRun(ctx, "start", ws.ContainerID); err != nil {
		d.Destroy(ctx, ws)
		return nil, fmt.Errorf("docker start: %w", err)
	}

	// Clone repo inside container
	if opts.RepoURL != "" {
		cloneCmd := []string{"git", "clone", "--branch", opts.BaseRef, "--single-branch", "--depth=1", opts.RepoURL, "."}
		if _, err := d.Exec(ctx, ws, cloneCmd); err != nil {
			d.Destroy(ctx, ws)
			return nil, fmt.Errorf("git clone: %w", err)
		}

		// Create working branch
		if opts.Branch != "" {
			if _, err := d.Exec(ctx, ws, []string{"git", "checkout", "-b", opts.Branch}); err != nil {
				d.Destroy(ctx, ws)
				return nil, fmt.Errorf("git checkout: %w", err)
			}
		}
	}

	return ws, nil
}

func (d *Docker) Exec(ctx context.Context, ws *Workspace, cmd []string) (*ExecResult, error) {
	args := append([]string{"exec", ws.ContainerID}, cmd...)
	c := exec.CommandContext(ctx, "docker", args...)

	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr

	err := c.Run()
	result := &ExecResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("docker exec: %w", err)
	}
	return result, nil
}

func (d *Docker) Destroy(ctx context.Context, ws *Workspace) error {
	_ = dockerRun(ctx, "rm", "-f", ws.ContainerID)
	return nil
}

func dockerRun(ctx context.Context, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%w: %s", err, out.String())
	}
	return nil
}

var counter atomic.Uint64

func uniqueCounter() uint64 {
	return counter.Add(1)
}
