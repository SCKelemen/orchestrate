package sandbox

import "context"

// Workspace represents an isolated execution environment.
type Workspace struct {
	ID          string
	ContainerID string
	WorkDir     string
	Branch      string
	RepoURL     string
}

// CreateOpts are options for creating a sandbox workspace.
type CreateOpts struct {
	Image                string
	RepoURL              string
	BaseRef              string
	Branch               string
	EnvVars              map[string]string
	VisibleRepoPaths     []string
	NetworkMode          NetworkMode
	AllowedEgressDomains []string
}

// ExecResult holds the output of a command execution in a sandbox.
type ExecResult struct {
	ExitCode int
	Stdout   string
	Stderr   string
}

// Sandbox manages isolated execution environments.
type Sandbox interface {
	Create(ctx context.Context, opts CreateOpts) (*Workspace, error)
	Exec(ctx context.Context, ws *Workspace, cmd []string) (*ExecResult, error)
	Destroy(ctx context.Context, ws *Workspace) error
}
