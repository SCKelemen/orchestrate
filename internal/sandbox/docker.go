package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"sync/atomic"
)

const (
	defaultContainerUser = "1000:1000"
	defaultPIDsLimit     = "512"
	defaultAgentImage    = "orchestrate-agent:latest"
)

// NetworkMode controls the container network configuration.
type NetworkMode string

const (
	NetworkModeDefault NetworkMode = "default"
	NetworkModeNone    NetworkMode = "none"
)

var defaultSandboxEnv = map[string]string{
	"HOME":            "/tmp",
	"TMPDIR":          "/tmp",
	"XDG_CONFIG_HOME": "/tmp/.config",
	"XDG_CACHE_HOME":  "/tmp/.cache",
}

// Docker implements Sandbox using Docker containers via os/exec.
type Docker struct {
	dataDir       string
	allowAnyImage bool
	allowedImages map[string]struct{}
	networkMode   NetworkMode
}

// DockerOption configures Docker sandbox behavior.
type DockerOption func(*Docker)

// WithAllowAnyImage disables image allowlist enforcement.
func WithAllowAnyImage(allow bool) DockerOption {
	return func(d *Docker) {
		d.allowAnyImage = allow
	}
}

// WithAllowedImages configures the explicit image allowlist.
func WithAllowedImages(images []string) DockerOption {
	return func(d *Docker) {
		d.allowedImages = make(map[string]struct{}, len(images))
		for _, image := range images {
			image = strings.TrimSpace(image)
			if image == "" {
				continue
			}
			d.allowedImages[image] = struct{}{}
		}
	}
}

// WithNetworkMode configures the Docker network mode.
func WithNetworkMode(mode NetworkMode) DockerOption {
	return func(d *Docker) {
		d.networkMode = mode
	}
}

// NewDocker creates a Docker sandbox manager.
func NewDocker(dataDir string, opts ...DockerOption) *Docker {
	d := &Docker{
		dataDir:       dataDir,
		allowedImages: map[string]struct{}{defaultAgentImage: {}},
		networkMode:   NetworkModeDefault,
	}
	for _, opt := range opts {
		opt(d)
	}
	if !d.allowAnyImage && len(d.allowedImages) == 0 {
		d.allowedImages[defaultAgentImage] = struct{}{}
	}
	if d.networkMode == "" {
		d.networkMode = NetworkModeDefault
	}
	return d
}

func (d *Docker) Create(ctx context.Context, opts CreateOpts) (*Workspace, error) {
	image := strings.TrimSpace(opts.Image)
	if image == "" {
		image = defaultAgentImage
	}
	opts.Image = image
	if !d.imageAllowed(image) {
		return nil, fmt.Errorf("sandbox image %q is not allowed", image)
	}

	ws := &Workspace{
		ID:      fmt.Sprintf("orch-%d", uniqueCounter()),
		Branch:  opts.Branch,
		RepoURL: opts.RepoURL,
	}

	args := d.createArgs(opts, ws.ID)

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

func (d *Docker) createArgs(opts CreateOpts, workspaceID string) []string {
	args := []string{
		"create",
		"--name", workspaceID,
		"--label", "orchestrate=true",
		"--user", defaultContainerUser,
		"--cap-drop", "ALL",
		"--security-opt", "no-new-privileges",
		"--read-only",
		"--pids-limit", defaultPIDsLimit,
		"--tmpfs", "/tmp:rw,nosuid,nodev",
		"--tmpfs", "/home/agent/workspace:rw,nosuid,nodev,uid=1000,gid=1000",
		"-w", "/home/agent/workspace",
	}
	if d.networkMode == NetworkModeNone {
		args = append(args, "--network", "none")
	}

	env := make(map[string]string, len(defaultSandboxEnv)+len(opts.EnvVars))
	for k, v := range defaultSandboxEnv {
		env[k] = v
	}
	for k, v := range opts.EnvVars {
		env[k] = v
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		args = append(args, "-e", k+"="+env[k])
	}

	args = append(args, opts.Image, "sleep", "infinity")
	return args
}

func (d *Docker) imageAllowed(image string) bool {
	if d.allowAnyImage {
		return true
	}
	_, ok := d.allowedImages[image]
	return ok
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
