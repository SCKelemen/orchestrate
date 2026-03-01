package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/url"
	"os/exec"
	"path"
	"sort"
	"strconv"
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
	NetworkModeDefault   NetworkMode = "default"
	NetworkModeNone      NetworkMode = "none"
	NetworkModeAllowlist NetworkMode = "allowlist"
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
	if opts.NetworkMode == NetworkModeNone && len(opts.AllowedEgressDomains) > 0 {
		return nil, fmt.Errorf("allowed egress domains cannot be used with network mode none")
	}
	effectiveMode := d.effectiveNetworkMode(opts.NetworkMode)
	if effectiveMode == NetworkModeAllowlist {
		if len(opts.AllowedEgressDomains) == 0 {
			return nil, fmt.Errorf("network mode allowlist requires allowed egress domains")
		}
		if opts.RepoURL != "" {
			host := extractHostFromRepoURL(opts.RepoURL)
			if host != "" && !domainListIncludesHost(opts.AllowedEgressDomains, host) {
				return nil, fmt.Errorf("allowed egress domains must include repo host %q", host)
			}
		}
		for _, envKey := range []string{"ANTHROPIC_BASE_URL", "OPENAI_BASE_URL"} {
			v := strings.TrimSpace(opts.EnvVars[envKey])
			if v == "" {
				continue
			}
			host := extractHostFromURL(v)
			if host != "" && !domainListIncludesHost(opts.AllowedEgressDomains, host) {
				return nil, fmt.Errorf("allowed egress domains must include %s host %q", envKey, host)
			}
		}
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

		if len(opts.VisibleRepoPaths) > 0 {
			paths, err := sanitizeVisiblePaths(opts.VisibleRepoPaths)
			if err != nil {
				d.Destroy(ctx, ws)
				return nil, err
			}
			if len(paths) > 0 {
				if _, err := d.Exec(ctx, ws, []string{"git", "sparse-checkout", "init", "--cone"}); err != nil {
					d.Destroy(ctx, ws)
					return nil, fmt.Errorf("git sparse-checkout init: %w", err)
				}
				sparseCmd := append([]string{"git", "sparse-checkout", "set"}, paths...)
				if _, err := d.Exec(ctx, ws, sparseCmd); err != nil {
					d.Destroy(ctx, ws)
					return nil, fmt.Errorf("git sparse-checkout set: %w", err)
				}
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
	if d.effectiveNetworkMode(opts.NetworkMode) == NetworkModeNone {
		args = append(args, "--network", "none")
	}

	env := make(map[string]string, len(defaultSandboxEnv)+len(opts.EnvVars))
	for k, v := range defaultSandboxEnv {
		env[k] = v
	}
	for k, v := range opts.EnvVars {
		env[k] = v
	}
	if len(opts.AllowedEgressDomains) > 0 {
		allowed := append([]string(nil), opts.AllowedEgressDomains...)
		sort.Strings(allowed)
		env["ORCHESTRATE_EGRESS_ALLOWLIST"] = strings.Join(allowed, ",")
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

func (d *Docker) effectiveNetworkMode(requested NetworkMode) NetworkMode {
	if d.networkMode == NetworkModeNone {
		return NetworkModeNone
	}
	switch requested {
	case "", NetworkModeDefault:
		return d.networkMode
	case NetworkModeNone:
		return NetworkModeNone
	case NetworkModeAllowlist:
		return NetworkModeAllowlist
	default:
		return d.networkMode
	}
}

func sanitizeVisiblePaths(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, raw := range in {
		p := strings.TrimSpace(raw)
		if p == "" {
			continue
		}
		p = filepathToSlash(p)
		if strings.HasPrefix(p, "/") {
			return nil, fmt.Errorf("visible repo path must be relative: %s", raw)
		}
		p = path.Clean(p)
		if p == "." {
			return nil, nil
		}
		if p == ".." || strings.HasPrefix(p, "../") {
			return nil, fmt.Errorf("visible repo path cannot escape repo root: %s", raw)
		}
		if p == ".git" || strings.HasPrefix(p, ".git/") {
			return nil, fmt.Errorf("visible repo path cannot target .git: %s", raw)
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

func filepathToSlash(v string) string {
	return strings.ReplaceAll(v, "\\", "/")
}

func extractHostFromRepoURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err == nil {
			return strings.ToLower(strings.TrimSpace(u.Hostname()))
		}
	}
	if at := strings.LastIndex(raw, "@"); at != -1 {
		rest := raw[at+1:]
		if i := strings.Index(rest, ":"); i > 0 {
			return strings.ToLower(strings.TrimSpace(rest[:i]))
		}
	}
	return ""
}

func extractHostFromURL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(u.Hostname()))
}

func domainListIncludesHost(allow []string, host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return false
	}
	for _, entry := range allow {
		entry = strings.TrimSpace(strings.ToLower(entry))
		if entry == host {
			return true
		}
		if h, _, err := net.SplitHostPort(entry); err == nil && h == host {
			return true
		}
		if strings.Count(entry, ":") == 1 && !strings.Contains(entry, "]") {
			i := strings.LastIndex(entry, ":")
			if i > 0 {
				if _, err := strconv.Atoi(entry[i+1:]); err == nil && entry[:i] == host {
					return true
				}
			}
		}
	}
	return false
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
