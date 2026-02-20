package command

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"github.com/SCKelemen/clix/v2"
	"github.com/SCKelemen/orchestrate/internal/agent"
	"github.com/SCKelemen/orchestrate/internal/api"
	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/orchestrator"
	"github.com/SCKelemen/orchestrate/internal/sandbox"
)

func newServerCmd() *clix.Command {
	cmd := clix.NewCommand("server")
	cmd.Short = "Start the orchestrate API server"

	var (
		addr          string
		maxConcurrent int
	)

	cmd.Flags.StringVar(clix.StringVarOptions{
		FlagOptions: clix.FlagOptions{Name: "addr", Short: "a", EnvVar: "ORCHESTRATE_ADDR"},
		Default:     ":8080",
		Value:       &addr,
	})
	cmd.Flags.IntVar(clix.IntVarOptions{
		FlagOptions: clix.FlagOptions{Name: "max-concurrent", Short: "c", EnvVar: "ORCHESTRATE_MAX_CONCURRENT"},
		Default:     "0",
		Value:       &maxConcurrent,
	})

	cmd.Run = func(ctx *clix.Context) error {
		logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		dir := dataDir()

		// Ensure subdirectories
		for _, sub := range []string{"repos", "workspaces", "logs"} {
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
				return fmt.Errorf("create %s dir: %w", sub, err)
			}
		}

		s, err := openStore()
		if err != nil {
			return fmt.Errorf("open store: %w", err)
		}
		defer s.Close()

		// Load or generate bearer auth token (backward compatibility)
		token := os.Getenv("ORCHESTRATE_TOKEN")
		if token == "" {
			tokenPath := filepath.Join(dir, "token")
			data, err := os.ReadFile(tokenPath)
			if err != nil {
				b := make([]byte, 32)
				rand.Read(b)
				token = hex.EncodeToString(b)
				os.WriteFile(tokenPath, []byte(token), 0o600)
				logger.Info("generated auth token", "path", tokenPath)
			} else {
				token = string(data)
			}
		}

		// Load or generate JWT signing secret
		jwtSecret, err := loadOrGenerateSecret(dir)
		if err != nil {
			return fmt.Errorf("jwt secret: %w", err)
		}
		signer := auth.NewSigner(jwtSecret, "orchestrate")

		// Build auth provider chain: JWT first, then static bearer
		providers := []auth.Provider{
			auth.NewJWTProvider(signer),
			auth.NewBearerProvider(token),
		}
		mw := auth.NewMiddleware(providers...)

		sb := sandbox.NewDocker(dir)
		ag := agent.NewClaude(sb)
		orch := orchestrator.New(s, sb, ag, dir, logger)
		sched := orchestrator.NewScheduler(s, orch, orchestrator.SchedulerOpts{
			MaxConcurrent: maxConcurrent,
		}, logger)

		// Start scheduler in background
		go func() {
			if err := sched.Run(ctx); err != nil {
				logger.Error("scheduler stopped", "error", err)
			}
		}()

		srv := api.NewServer(s, mw, signer, logger)

		tokenPreview := token
		if len(tokenPreview) > 8 {
			tokenPreview = tokenPreview[:8] + "..."
		}
		logger.Info("server starting", "addr", addr, "token", tokenPreview)

		server := &http.Server{Addr: addr, Handler: srv}
		go func() {
			<-ctx.Done()
			server.Close()
		}()

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			return fmt.Errorf("server: %w", err)
		}
		return nil
	}

	return cmd
}

// loadOrGenerateSecret loads or generates a 32-byte JWT signing key.
// Stored at ~/.local/share/orchestrate/jwt.key with mode 0600.
func loadOrGenerateSecret(dir string) ([]byte, error) {
	path := filepath.Join(dir, "jwt.key")
	data, err := os.ReadFile(path)
	if err == nil && len(data) == 32 {
		return data, nil
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	if err := os.WriteFile(path, secret, 0o600); err != nil {
		return nil, fmt.Errorf("write secret: %w", err)
	}
	return secret, nil
}
