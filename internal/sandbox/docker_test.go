package sandbox

import (
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
