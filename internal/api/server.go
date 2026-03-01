package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/store"
)

const maxRequestBodyBytes = 1 << 20 // 1 MiB

// Server is the HTTP API server for orchestrate.
type Server struct {
	store             *store.Store
	auth              *auth.Middleware
	signer            *auth.Signer
	allowInsecureAuth bool
	allowAnyImage     bool
	allowedImages     map[string]struct{}
	webauthn          *auth.WebAuthnProvider
	webauthnSessions  *auth.WebAuthnSessionStore
	mux               *http.ServeMux
	logger            *slog.Logger
}

// ServerOption configures the server.
type ServerOption func(*Server)

// WithWebAuthn enables WebAuthn support.
func WithWebAuthn(wp *auth.WebAuthnProvider) ServerOption {
	return func(s *Server) {
		s.webauthn = wp
		s.webauthnSessions = auth.NewWebAuthnSessionStore()
	}
}

// WithInsecureEmailAuth enables unauthenticated email-based login flows.
// This should remain disabled in public deployments.
func WithInsecureEmailAuth(enabled bool) ServerOption {
	return func(s *Server) {
		s.allowInsecureAuth = enabled
	}
}

// WithImagePolicy configures image allowlist enforcement for task and schedule submissions.
func WithImagePolicy(allowed []string, allowAny bool) ServerOption {
	return func(s *Server) {
		s.allowAnyImage = allowAny
		s.allowedImages = make(map[string]struct{}, len(allowed))
		for _, image := range allowed {
			image = strings.TrimSpace(image)
			if image == "" {
				continue
			}
			s.allowedImages[image] = struct{}{}
		}
	}
}

// NewServer creates a new API server.
func NewServer(s *store.Store, mw *auth.Middleware, signer *auth.Signer, logger *slog.Logger, opts ...ServerOption) *Server {
	srv := &Server{
		store:         s,
		auth:          mw,
		signer:        signer,
		allowedImages: map[string]struct{}{defaultAgentImage: {}},
		mux:           http.NewServeMux(),
		logger:        logger,
	}
	for _, opt := range opts {
		opt(srv)
	}
	if !srv.allowAnyImage && len(srv.allowedImages) == 0 {
		srv.allowedImages[defaultAgentImage] = struct{}{}
	}
	srv.routes()
	srv.registerAuthRoutes()
	return srv
}

func (s *Server) routes() {
	// Task standard methods (AIP-131..135)
	s.mux.HandleFunc("POST /v1/tasks", s.auth.Wrap(s.createTask))
	s.mux.HandleFunc("GET /v1/tasks", s.auth.Wrap(s.listTasks))

	// Single task: GET, PATCH, DELETE, and POST custom methods
	// POST to /v1/tasks/{id} is routed to custom method dispatcher (cancel, retry)
	s.mux.HandleFunc("GET /v1/tasks/{task}", s.auth.Wrap(s.getTask))
	s.mux.HandleFunc("PATCH /v1/tasks/{task}", s.auth.Wrap(s.updateTask))
	s.mux.HandleFunc("DELETE /v1/tasks/{task}", s.auth.Wrap(s.deleteTask))

	// Run sub-resource
	s.mux.HandleFunc("GET /v1/tasks/{task}/runs", s.auth.Wrap(s.listRuns))
	s.mux.HandleFunc("GET /v1/tasks/{task}/runs/{run}", s.auth.Wrap(s.getRun))

	// Custom methods (AIP-136) and run logs use path suffix with colon.
	// Go's ServeMux doesn't support colons in wildcards, so we handle these
	// via a catch-all POST handler that parses the action from the path.
	s.mux.HandleFunc("POST /v1/tasks/{task}/{action}", s.auth.Wrap(s.taskCustomMethod))
	s.mux.HandleFunc("GET /v1/tasks/{task}/runs/{run}/{action}", s.auth.Wrap(s.runCustomMethod))

	// Schedule standard methods
	s.mux.HandleFunc("POST /v1/schedules", s.auth.Wrap(s.createSchedule))
	s.mux.HandleFunc("GET /v1/schedules", s.auth.Wrap(s.listSchedules))
	s.mux.HandleFunc("GET /v1/schedules/{schedule}", s.auth.Wrap(s.getSchedule))
	s.mux.HandleFunc("PATCH /v1/schedules/{schedule}", s.auth.Wrap(s.updateSchedule))
	s.mux.HandleFunc("DELETE /v1/schedules/{schedule}", s.auth.Wrap(s.deleteSchedule))

	// Schedule custom methods (AIP-136)
	s.mux.HandleFunc("POST /v1/schedules/{schedule}/{action}", s.auth.Wrap(s.scheduleCustomMethod))

	// Health
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// taskCustomMethod dispatches AIP-136 custom methods like :cancel, :retry.
func (s *Server) taskCustomMethod(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case ":cancel":
		s.cancelTask(w, r)
	case ":retry":
		s.retryTask(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

// runCustomMethod dispatches custom methods on runs like :logs.
func (s *Server) runCustomMethod(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case ":logs":
		s.streamLogs(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

// ServeHTTP implements http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Body != nil {
		r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
	}
	s.mux.ServeHTTP(w, r)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]any{
		"error": map[string]any{
			"code":    code,
			"message": msg,
		},
	})
}

// scheduleCustomMethod dispatches AIP-136 custom methods for schedules.
func (s *Server) scheduleCustomMethod(w http.ResponseWriter, r *http.Request) {
	action := r.PathValue("action")
	switch action {
	case ":pause":
		s.pauseSchedule(w, r)
	case ":resume":
		s.resumeSchedule(w, r)
	default:
		writeError(w, http.StatusNotFound, "unknown action: "+action)
	}
}

func nowUTC() time.Time {
	return time.Now().UTC()
}
