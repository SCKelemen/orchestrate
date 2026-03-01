package command

import (
	"context"
	"os"
	"path/filepath"

	"github.com/SCKelemen/clix/v2"
	"github.com/SCKelemen/orchestrate/internal/store"
)

const version = "0.1.0"

// dataDir returns the base data directory for orchestrate.
func dataDir() string {
	if d := os.Getenv("ORCHESTRATE_DATA_DIR"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "orchestrate")
}

// openStore opens the SQLite store in the data directory.
func openStore() (*store.Store, error) {
	dir := dataDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return store.Open(filepath.Join(dir, "orchestrate.db"))
}

// Run creates and executes the CLI application.
func Run(ctx context.Context, args []string) error {
	app := clix.NewApp("orchestrate")
	app.Version = version
	app.Description = "Orchestrate parallel Claude Code agents"

	root := clix.NewCommand("orchestrate")
	root.Short = "Orchestrate parallel Claude Code agents"
	root.Children = []*clix.Command{
		newServerCmd(),
		newSubmitCmd(),
		newListCmd(),
		newStatusCmd(),
		newCancelCmd(),
		newLogsCmd(),
		newScheduleCmd(),
		newAuthCmd(),
	}

	app.Root = root
	return app.Run(ctx, args)
}
