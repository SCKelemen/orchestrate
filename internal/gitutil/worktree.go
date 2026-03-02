package gitutil

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// RepoCache manages bare clones for efficient worktree creation.
type RepoCache struct {
	dir string // base directory for bare clones
}

// NewRepoCache creates a new repo cache at the given directory.
func NewRepoCache(dir string) *RepoCache {
	return &RepoCache{dir: dir}
}

// EnsureClone ensures a bare clone of the repo exists and is up to date.
// Returns the path to the bare clone.
func (rc *RepoCache) EnsureClone(ctx context.Context, repoURL string) (string, error) {
	// Derive a directory name from the repo URL
	name := repoName(repoURL)
	clonePath := filepath.Join(rc.dir, name+".git")

	if _, err := os.Stat(clonePath); os.IsNotExist(err) {
		// Fresh bare clone
		if err := os.MkdirAll(rc.dir, 0o750); err != nil {
			return "", fmt.Errorf("mkdir repos: %w", err)
		}
		if err := gitRun(ctx, rc.dir, "clone", "--bare", repoURL, clonePath); err != nil {
			return "", fmt.Errorf("bare clone: %w", err)
		}
	} else {
		// Fetch latest
		if err := gitRun(ctx, clonePath, "fetch", "--all", "--prune"); err != nil {
			return "", fmt.Errorf("fetch: %w", err)
		}
	}

	return clonePath, nil
}

// CreateWorktree creates a git worktree from a bare clone.
func (rc *RepoCache) CreateWorktree(ctx context.Context, barePath, worktreeDir, baseRef, branch string) error {
	if err := os.MkdirAll(filepath.Dir(worktreeDir), 0o750); err != nil {
		return fmt.Errorf("mkdir worktree parent: %w", err)
	}

	// Create worktree from the base ref
	if err := gitRun(ctx, barePath, "worktree", "add", "-b", branch, worktreeDir, baseRef); err != nil {
		return fmt.Errorf("worktree add: %w", err)
	}
	return nil
}

// RemoveWorktree removes a git worktree.
func (rc *RepoCache) RemoveWorktree(ctx context.Context, barePath, worktreeDir string) error {
	_ = gitRun(ctx, barePath, "worktree", "remove", "--force", worktreeDir)
	_ = os.RemoveAll(worktreeDir)
	return nil
}

func gitRun(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...) // #nosec G204 -- args are constructed internally, not from user input
	cmd.Dir = dir
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s: %w: %s", args[0], err, out.String())
	}
	return nil
}

// repoName derives a safe directory name from a repo URL.
func repoName(url string) string {
	// Handle both HTTPS and SSH URLs
	name := url
	name = strings.TrimSuffix(name, ".git")
	// Take last two path components (org/repo)
	parts := strings.Split(name, "/")
	if len(parts) >= 2 {
		name = parts[len(parts)-2] + "_" + parts[len(parts)-1]
	} else if len(parts) >= 1 {
		name = parts[len(parts)-1]
	}
	// Also handle ssh git@github.com:org/repo
	if i := strings.LastIndex(name, ":"); i >= 0 {
		name = strings.Replace(name[i+1:], "/", "_", -1)
	}
	return name
}
