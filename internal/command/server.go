package command

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

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
			if err := os.MkdirAll(filepath.Join(dir, sub), 0o700); err != nil {
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
				if _, err := rand.Read(b); err != nil {
					return fmt.Errorf("generate auth token: %w", err)
				}
				token = hex.EncodeToString(b)
				if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
					return fmt.Errorf("write auth token: %w", err)
				}
				logger.Info("generated auth token", "path", tokenPath)
			} else {
				token = strings.TrimSpace(string(data))
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
		agentBackend := os.Getenv("ORCHESTRATE_AGENT")
		defaultAgent, err := agent.NormalizeBackend(agentBackend)
		if err != nil {
			return fmt.Errorf("invalid ORCHESTRATE_AGENT: %w", err)
		}
		logger.Info("agent backend configured", "defaultAgent", defaultAgent)
		orch := orchestrator.New(s, sb, agent.NewBackends(sb), defaultAgent, dir, logger)
		sched := orchestrator.NewScheduler(s, orch, orchestrator.SchedulerOpts{
			MaxConcurrent: maxConcurrent,
		}, logger)

		// Start scheduler in background
		go func() {
			if err := sched.Run(ctx); err != nil {
				logger.Error("scheduler stopped", "error", err)
			}
		}()

		// Configure WebAuthn if ORCHESTRATE_WEBAUTHN_RPID is set.
		var serverOpts []api.ServerOption
		enableInsecureAuth := strings.EqualFold(os.Getenv("ORCHESTRATE_ENABLE_EMAIL_AUTH"), "1") ||
			strings.EqualFold(os.Getenv("ORCHESTRATE_ENABLE_EMAIL_AUTH"), "true")
		serverOpts = append(serverOpts, api.WithInsecureEmailAuth(enableInsecureAuth))
		if enableInsecureAuth {
			logger.Warn("insecure email-based auth flows are enabled")
		}

		if rpID := os.Getenv("ORCHESTRATE_WEBAUTHN_RPID"); rpID != "" {
			rpName := os.Getenv("ORCHESTRATE_WEBAUTHN_RPNAME")
			if rpName == "" {
				rpName = "Orchestrate"
			}
			rpOrigins := strings.Split(os.Getenv("ORCHESTRATE_WEBAUTHN_ORIGINS"), ",")
			wp, err := auth.NewWebAuthnProvider(auth.WebAuthnConfig{
				RPDisplayName: rpName,
				RPID:          rpID,
				RPOrigins:     rpOrigins,
			})
			if err != nil {
				return fmt.Errorf("webauthn: %w", err)
			}
			serverOpts = append(serverOpts, api.WithWebAuthn(wp))
			logger.Info("webauthn enabled", "rpid", rpID)
		}

		srv := api.NewServer(s, mw, signer, logger, serverOpts...)

		logger.Info("server starting", "addr", addr)

		server := &http.Server{
			Addr:              addr,
			Handler:           srv,
			ReadHeaderTimeout: 5 * time.Second,
			ReadTimeout:       30 * time.Second,
			WriteTimeout:      60 * time.Second,
			IdleTimeout:       120 * time.Second,
		}
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
