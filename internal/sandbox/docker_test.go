package sandbox

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestUniqueCounterIncrements(t *testing.T) {
	t.Parallel()

	a := uniqueCounter()
	b := uniqueCounter()
	if b <= a {
		t.Fatalf("counter did not increment: a=%d b=%d", a, b)
	}
}

func TestNewDocker(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate")
	if d == nil {
		t.Fatal("docker sandbox is nil")
	}
	if d.dataDir != "/tmp/orchestrate" {
		t.Fatalf("dataDir=%q want=%q", d.dataDir, "/tmp/orchestrate")
	}
	if !d.imageAllowed("orchestrate-agent:latest") {
		t.Fatal("default image should be allowed")
	}
	if d.imageAllowed("custom/image:latest") {
		t.Fatal("custom image should not be allowed by default")
	}
}

func TestCreateArgsIncludesHardeningDefaults(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate")
	args := d.createArgs(CreateOpts{
		Image: "orchestrate-agent:latest",
		EnvVars: map[string]string{
			"OPENAI_API_KEY": "test-key",
		},
	}, "orch-1")

	requiredPairs := [][2]string{
		{"--name", "orch-1"},
		{"--user", "1000:1000"},
		{"--cap-drop", "ALL"},
		{"--security-opt", "no-new-privileges"},
		{"--pids-limit", "512"},
		{"--tmpfs", "/tmp:rw,nosuid,nodev"},
		{"--tmpfs", "/home/agent/workspace:rw,nosuid,nodev,uid=1000,gid=1000"},
		{"-w", "/home/agent/workspace"},
	}
	for _, pair := range requiredPairs {
		if !containsArgPair(args, pair[0], pair[1]) {
			t.Fatalf("missing required arg pair %q %q in %#v", pair[0], pair[1], args)
		}
	}

	if !containsArg(args, "--read-only") {
		t.Fatalf("missing --read-only in %#v", args)
	}

	env := parseEnvArgs(args)
	if env["HOME"] != "/tmp" {
		t.Fatalf("HOME=%q want=/tmp", env["HOME"])
	}
	if env["TMPDIR"] != "/tmp" {
		t.Fatalf("TMPDIR=%q want=/tmp", env["TMPDIR"])
	}
	if env["XDG_CONFIG_HOME"] != "/tmp/.config" {
		t.Fatalf("XDG_CONFIG_HOME=%q want=/tmp/.config", env["XDG_CONFIG_HOME"])
	}
	if env["XDG_CACHE_HOME"] != "/tmp/.cache" {
		t.Fatalf("XDG_CACHE_HOME=%q want=/tmp/.cache", env["XDG_CACHE_HOME"])
	}
	if env["OPENAI_API_KEY"] != "test-key" {
		t.Fatalf("OPENAI_API_KEY=%q want=test-key", env["OPENAI_API_KEY"])
	}

	last := strings.Join(args[len(args)-3:], " ")
	if last != "orchestrate-agent:latest sleep infinity" {
		t.Fatalf("tail=%q want %q", last, "orchestrate-agent:latest sleep infinity")
	}
}

func TestCreateArgsAllowsEnvOverride(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate")
	args := d.createArgs(CreateOpts{
		Image: "orchestrate-agent:latest",
		EnvVars: map[string]string{
			"HOME": "/home/agent/workspace",
		},
	}, "orch-2")

	env := parseEnvArgs(args)
	if env["HOME"] != "/home/agent/workspace" {
		t.Fatalf("HOME=%q want=/home/agent/workspace", env["HOME"])
	}
}

func TestCreateArgsWithNetworkNone(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate", WithNetworkMode(NetworkModeNone))
	args := d.createArgs(CreateOpts{
		Image: "orchestrate-agent:latest",
	}, "orch-3")

	if !containsArgPair(args, "--network", "none") {
		t.Fatalf("expected --network none in %#v", args)
	}
}

func TestCreateArgsWithNetworkAllowlistDoesNotDisableNetwork(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate")
	args := d.createArgs(CreateOpts{
		Image:                "orchestrate-agent:latest",
		NetworkMode:          NetworkModeAllowlist,
		AllowedEgressDomains: []string{"github.com:443", "api.openai.com:443"},
	}, "orch-allow")

	if containsArgPair(args, "--network", "none") {
		t.Fatalf("did not expect --network none for allowlist mode: %#v", args)
	}
	env := parseEnvArgs(args)
	if env["ORCHESTRATE_EGRESS_ALLOWLIST"] != "api.openai.com:443,github.com:443" {
		t.Fatalf("ORCHESTRATE_EGRESS_ALLOWLIST=%q", env["ORCHESTRATE_EGRESS_ALLOWLIST"])
	}
}

func TestWithAllowAnyImage(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate", WithAllowAnyImage(true))
	if !d.imageAllowed("custom/image:latest") {
		t.Fatal("custom image should be allowed when allowAnyImage is enabled")
	}
}

func TestWithAllowedImages(t *testing.T) {
	t.Parallel()

	d := NewDocker("/tmp/orchestrate", WithAllowedImages([]string{"ghcr.io/acme/orchestrate-agent:v1"}))
	if d.imageAllowed("orchestrate-agent:latest") {
		t.Fatal("default image should not be allowed after explicit allowlist override")
	}
	if !d.imageAllowed("ghcr.io/acme/orchestrate-agent:v1") {
		t.Fatal("configured image should be allowed")
	}
}

func TestSanitizeVisiblePaths(t *testing.T) {
	t.Parallel()

	paths, err := sanitizeVisiblePaths([]string{"src", "./pkg", "src", "tests"})
	if err != nil {
		t.Fatalf("sanitizeVisiblePaths: %v", err)
	}
	if len(paths) != 3 {
		t.Fatalf("len=%d want=3 paths=%v", len(paths), paths)
	}
}

func TestSanitizeVisiblePathsRejectsTraversal(t *testing.T) {
	t.Parallel()

	if _, err := sanitizeVisiblePaths([]string{"../secret"}); err == nil {
		t.Fatal("expected traversal error")
	}
}

func TestDomainListIncludesHost(t *testing.T) {
	t.Parallel()

	allow := []string{"github.com:443", "api.openai.com"}
	if !domainListIncludesHost(allow, "github.com") {
		t.Fatal("github.com should be allowed")
	}
	if domainListIncludesHost(allow, "example.com") {
		t.Fatal("example.com should not be allowed")
	}
}

func containsArg(args []string, want string) bool {
	for _, a := range args {
		if a == want {
			return true
		}
	}
	return false
}

func containsArgPair(args []string, key, value string) bool {
	for i := 0; i < len(args)-1; i++ {
		if args[i] == key && args[i+1] == value {
			return true
		}
	}
	return false
}

func parseEnvArgs(args []string) map[string]string {
	env := map[string]string{}
	for i := 0; i < len(args)-1; i++ {
		if args[i] != "-e" {
			continue
		}
		parts := strings.SplitN(args[i+1], "=", 2)
		if len(parts) != 2 {
			continue
		}
		env[parts[0]] = parts[1]
	}
	return env
}

// --- fakeExec test helper ---

type execCall struct {
	name string
	args []string
}

type execResult struct {
	stdout   string
	stderr   string
	exitCode int
	err      error
}

// fakeExecFn returns an execFn that records calls and returns scripted results.
func fakeExecFn(results []execResult) (execFn, *[]execCall) {
	var calls []execCall
	idx := 0
	fn := func(_ context.Context, name string, args ...string) (string, string, int, error) {
		calls = append(calls, execCall{name: name, args: append([]string{}, args...)})
		if idx < len(results) {
			r := results[idx]
			idx++
			return r.stdout, r.stderr, r.exitCode, r.err
		}
		idx++
		return "", "", 0, nil
	}
	return fn, &calls
}

// --- Create tests ---

func TestCreateFullFlow(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "abc123\n"}, // docker create
		{},                   // docker start
		{},                   // docker exec git clone
		{},                   // docker exec git checkout
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	ws, err := d.Create(context.Background(), CreateOpts{
		Image:   "agent:latest",
		RepoURL: "https://github.com/test/repo",
		BaseRef: "main",
		Branch:  "orchestrate/t1",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.ContainerID != "abc123" {
		t.Fatalf("ContainerID=%q want=abc123", ws.ContainerID)
	}
	if ws.Branch != "orchestrate/t1" {
		t.Fatalf("Branch=%q", ws.Branch)
	}
	if ws.RepoURL != "https://github.com/test/repo" {
		t.Fatalf("RepoURL=%q", ws.RepoURL)
	}

	c := *calls
	if len(c) != 4 {
		t.Fatalf("got %d calls, want 4", len(c))
	}

	// docker create
	if c[0].args[0] != "create" {
		t.Fatalf("call[0] args[0]=%q want=create", c[0].args[0])
	}

	// docker start <containerID>
	if c[1].args[0] != "start" || c[1].args[1] != "abc123" {
		t.Fatalf("call[1]=%v want=[start abc123]", c[1].args)
	}

	// docker exec <containerID> git clone ...
	if c[2].args[0] != "exec" || c[2].args[1] != "abc123" || !containsArg(c[2].args, "clone") {
		t.Fatalf("call[2]=%v want docker exec with git clone", c[2].args)
	}

	// docker exec <containerID> git checkout -b <branch>
	if !containsArg(c[3].args, "checkout") || !containsArg(c[3].args, "orchestrate/t1") {
		t.Fatalf("call[3]=%v want docker exec with git checkout", c[3].args)
	}
}

func TestCreateNoRepoSkipsClone(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "cid1\n"}, // docker create
		{},                 // docker start
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	ws, err := d.Create(context.Background(), CreateOpts{Image: "agent:latest"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.ContainerID != "cid1" {
		t.Fatalf("ContainerID=%q", ws.ContainerID)
	}
	if len(*calls) != 2 {
		t.Fatalf("got %d calls, want 2 (create + start)", len(*calls))
	}
}

func TestCreateNoBranchSkipsCheckout(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "cid2\n"}, // docker create
		{},                 // docker start
		{},                 // docker exec git clone
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	ws, err := d.Create(context.Background(), CreateOpts{
		Image:   "agent:latest",
		RepoURL: "https://github.com/test/repo",
		BaseRef: "main",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if ws.ContainerID != "cid2" {
		t.Fatalf("ContainerID=%q", ws.ContainerID)
	}
	if len(*calls) != 3 {
		t.Fatalf("got %d calls, want 3 (create + start + clone)", len(*calls))
	}
}

func TestCreateDockerCreateFails(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{err: errors.New("docker not found")},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	_, err := d.Create(context.Background(), CreateOpts{Image: "agent:latest"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "docker create") {
		t.Fatalf("error=%q want 'docker create' prefix", err.Error())
	}
	if len(*calls) != 1 {
		t.Fatalf("got %d calls, want 1", len(*calls))
	}
}

func TestCreateDockerStartFailsCleansUp(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "cid3\n"},               // docker create
		{exitCode: 1, stderr: "no room"}, // docker start fails
		{},                               // docker rm -f (cleanup)
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	_, err := d.Create(context.Background(), CreateOpts{Image: "agent:latest"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "docker start") {
		t.Fatalf("error=%q want 'docker start' prefix", err.Error())
	}
	// Verify cleanup: last call should be docker rm -f
	c := *calls
	if len(c) != 3 {
		t.Fatalf("got %d calls, want 3 (create + start + destroy)", len(c))
	}
	if c[2].args[0] != "rm" {
		t.Fatalf("cleanup call=%v want rm -f", c[2].args)
	}
}

func TestCreateGitCloneFailsCleansUp(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "cid4\n"},                      // docker create
		{},                                      // docker start
		{err: errors.New("connection refused")}, // docker exec git clone fails
		{},                                      // docker rm -f (cleanup)
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	_, err := d.Create(context.Background(), CreateOpts{
		Image:   "agent:latest",
		RepoURL: "https://github.com/test/repo",
		BaseRef: "main",
		Branch:  "feature",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "git clone") {
		t.Fatalf("error=%q want 'git clone' prefix", err.Error())
	}
	c := *calls
	last := c[len(c)-1]
	if last.args[0] != "rm" {
		t.Fatalf("last call=%v want rm -f (cleanup)", last.args)
	}
}

func TestCreateGitCheckoutFailsCleansUp(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "cid5\n"},                // docker create
		{},                                // docker start
		{},                                // docker exec git clone
		{err: errors.New("branch error")}, // docker exec git checkout fails
		{},                                // docker rm -f (cleanup)
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	_, err := d.Create(context.Background(), CreateOpts{
		Image:   "agent:latest",
		RepoURL: "https://github.com/test/repo",
		BaseRef: "main",
		Branch:  "feature",
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "git checkout") {
		t.Fatalf("error=%q want 'git checkout' prefix", err.Error())
	}
	c := *calls
	last := c[len(c)-1]
	if last.args[0] != "rm" {
		t.Fatalf("last call=%v want rm -f (cleanup)", last.args)
	}
}

// --- Exec tests ---

func TestExecSuccess(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{
		{stdout: "hello\n", stderr: "warn\n"},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	ws := &Workspace{ContainerID: "cid-exec"}
	res, err := d.Exec(context.Background(), ws, []string{"echo", "hello"})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("ExitCode=%d want=0", res.ExitCode)
	}
	if res.Stdout != "hello\n" {
		t.Fatalf("Stdout=%q", res.Stdout)
	}
	if res.Stderr != "warn\n" {
		t.Fatalf("Stderr=%q", res.Stderr)
	}

	c := *calls
	if c[0].name != "docker" {
		t.Fatalf("name=%q want=docker", c[0].name)
	}
	if c[0].args[0] != "exec" || c[0].args[1] != "cid-exec" {
		t.Fatalf("args=%v want [exec cid-exec ...]", c[0].args)
	}
	if c[0].args[2] != "echo" || c[0].args[3] != "hello" {
		t.Fatalf("cmd args=%v want [echo hello]", c[0].args[2:])
	}
}

func TestExecNonZeroExit(t *testing.T) {
	t.Parallel()

	fn, _ := fakeExecFn([]execResult{
		{stdout: "out", stderr: "err", exitCode: 42},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	res, err := d.Exec(context.Background(), &Workspace{ContainerID: "cid"}, []string{"false"})
	if err != nil {
		t.Fatalf("Exec should not return error for non-zero exit: %v", err)
	}
	if res.ExitCode != 42 {
		t.Fatalf("ExitCode=%d want=42", res.ExitCode)
	}
	if res.Stdout != "out" {
		t.Fatalf("Stdout=%q", res.Stdout)
	}
	if res.Stderr != "err" {
		t.Fatalf("Stderr=%q", res.Stderr)
	}
}

func TestExecSystemError(t *testing.T) {
	t.Parallel()

	fn, _ := fakeExecFn([]execResult{
		{err: errors.New("container gone")},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	_, err := d.Exec(context.Background(), &Workspace{ContainerID: "cid"}, []string{"ls"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "docker exec") {
		t.Fatalf("error=%q want 'docker exec' prefix", err.Error())
	}
}

// --- Destroy tests ---

func TestDestroyCallsRemove(t *testing.T) {
	t.Parallel()

	fn, calls := fakeExecFn([]execResult{{}})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	err := d.Destroy(context.Background(), &Workspace{ContainerID: "cid-rm"})
	if err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	c := *calls
	if len(c) != 1 {
		t.Fatalf("got %d calls, want 1", len(c))
	}
	if c[0].args[0] != "rm" || c[0].args[1] != "-f" || c[0].args[2] != "cid-rm" {
		t.Fatalf("args=%v want [rm -f cid-rm]", c[0].args)
	}
}

func TestDestroySwallowsError(t *testing.T) {
	t.Parallel()

	fn, _ := fakeExecFn([]execResult{
		{err: errors.New("already removed")},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	err := d.Destroy(context.Background(), &Workspace{ContainerID: "gone"})
	if err != nil {
		t.Fatalf("Destroy should swallow errors, got: %v", err)
	}
}

// --- dockerRun tests ---

func TestDockerRunSuccess(t *testing.T) {
	t.Parallel()

	fn, _ := fakeExecFn([]execResult{{}})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	err := d.dockerRun(context.Background(), "start", "cid")
	if err != nil {
		t.Fatalf("dockerRun: %v", err)
	}
}

func TestDockerRunNonZeroExit(t *testing.T) {
	t.Parallel()

	fn, _ := fakeExecFn([]execResult{
		{exitCode: 1, stderr: "error msg"},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	err := d.dockerRun(context.Background(), "start", "cid")
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if !strings.Contains(err.Error(), "exit status 1") {
		t.Fatalf("error=%q want 'exit status 1'", err.Error())
	}
}

func TestDockerRunSystemError(t *testing.T) {
	t.Parallel()

	fn, _ := fakeExecFn([]execResult{
		{err: errors.New("docker missing")},
	})
	d := &Docker{dataDir: "/tmp/orch", exec: fn, allowAnyImage: true}

	err := d.dockerRun(context.Background(), "start", "cid")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "docker missing") {
		t.Fatalf("error=%q want 'docker missing'", err.Error())
	}
}
