package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/schedule"
	"github.com/SCKelemen/orchestrate/internal/store"
)

type createScheduleRequest struct {
	Agent        string                    `json:"agent"`
	Title        string                    `json:"title"`
	Description  string                    `json:"description"`
	ScheduleExpr string                    `json:"scheduleExpr"`
	Prompt       string                    `json:"prompt"`
	RepoURL      string                    `json:"repoUrl"`
	BaseRef      string                    `json:"baseRef"`
	Strategy     string                    `json:"strategy"`
	AgentCount   int                       `json:"agentCount"`
	Image        string                    `json:"image"`
	MaxRuns      int                       `json:"maxRuns"`
	Manifest     *store.PermissionManifest `json:"manifest,omitempty"`
}

type updateScheduleRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
}

type scheduleResponse struct {
	Name         string                   `json:"name"`
	Agent        string                   `json:"agent"`
	Title        string                   `json:"title"`
	Description  string                   `json:"description"`
	ScheduleExpr string                   `json:"scheduleExpr"`
	ScheduleType string                   `json:"scheduleType"`
	Prompt       string                   `json:"prompt"`
	RepoURL      string                   `json:"repoUrl"`
	BaseRef      string                   `json:"baseRef"`
	Strategy     string                   `json:"strategy"`
	AgentCount   int                      `json:"agentCount"`
	Image        string                   `json:"image"`
	Manifest     store.PermissionManifest `json:"manifest,omitempty"`
	State        string                   `json:"state"`
	LastRunTime  *string                  `json:"lastRunTime"`
	NextRunTime  *string                  `json:"nextRunTime"`
	RunCount     int                      `json:"runCount"`
	MaxRuns      int                      `json:"maxRuns"`
	CreateTime   string                   `json:"createTime"`
}

func toScheduleResponse(sc *store.Schedule) scheduleResponse {
	return scheduleResponse{
		Name:         "schedules/" + sc.ID,
		Agent:        sc.Agent,
		Title:        sc.Title,
		Description:  sc.Description,
		ScheduleExpr: sc.ScheduleExpr,
		ScheduleType: sc.ScheduleType,
		Prompt:       sc.Prompt,
		RepoURL:      sc.RepoURL,
		BaseRef:      sc.BaseRef,
		Strategy:     string(sc.Strategy),
		AgentCount:   sc.AgentCount,
		Image:        sc.Image,
		Manifest:     parseStoredManifest(sc.Manifest),
		State:        string(sc.State),
		LastRunTime:  sc.LastRunTime,
		NextRunTime:  sc.NextRunTime,
		RunCount:     sc.RunCount,
		MaxRuns:      sc.MaxRuns,
		CreateTime:   sc.CreateTime,
	}
}

func authorizeScheduleAccess(w http.ResponseWriter, id *auth.Identity, sc *store.Schedule) bool {
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if isAdminIdentity(id) {
		return true
	}
	if sc.OwnerUserID != id.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

// AIP-131: CreateSchedule
func (s *Server) createSchedule(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if req.ScheduleExpr == "" {
		writeError(w, http.StatusBadRequest, "scheduleExpr is required")
		return
	}
	if req.Prompt == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}
	if err := validatePromptSize(req.Prompt); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.RepoURL == "" {
		writeError(w, http.StatusBadRequest, "repoUrl is required")
		return
	}
	strategy, err := normalizeStrategy(req.Strategy)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentCount, err := normalizeAgentCount(req.AgentCount)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	agentBackend, err := normalizeAgentBackend(req.Agent)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	image, err := normalizeImage(req.Image, s.allowedImages, s.allowAnyImage)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	_, manifestJSON, err := normalizeAndMarshalManifest(req.Manifest, req.RepoURL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// Validate and parse schedule expression
	spec, err := schedule.Parse(req.ScheduleExpr)
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("invalid scheduleExpr: %v", err))
		return
	}

	// Compute the first run time
	next := spec.Next(nowUTC())

	id := newID()
	sc, err := s.store.CreateSchedule(r.Context(), id, store.CreateScheduleParams{
		OwnerUserID:  idn.UserID,
		Agent:        agentBackend,
		Title:        req.Title,
		Description:  req.Description,
		ScheduleExpr: req.ScheduleExpr,
		ScheduleType: string(spec.Type),
		Prompt:       req.Prompt,
		RepoURL:      req.RepoURL,
		BaseRef:      req.BaseRef,
		Strategy:     strategy,
		AgentCount:   agentCount,
		Image:        image,
		Manifest:     manifestJSON,
		NextRunTime:  next.UTC().Format("2006-01-02T15:04:05Z"),
		MaxRuns:      req.MaxRuns,
	})
	if err != nil {
		s.logger.Error("create schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create schedule")
		return
	}
	writeJSON(w, http.StatusCreated, toScheduleResponse(sc))
}

// AIP-132: ListSchedules
func (s *Server) listSchedules(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	q := r.URL.Query()
	pageSize := 20
	if ps := q.Get("pageSize"); ps != "" {
		if n, err := strconv.Atoi(ps); err == nil {
			pageSize = n
		}
	}

	params := store.ListSchedulesParams{
		State:     store.ScheduleState(q.Get("state")),
		PageSize:  pageSize,
		PageToken: q.Get("pageToken"),
	}
	if !isAdminIdentity(idn) {
		params.OwnerUserID = idn.UserID
	}

	schedules, err := s.store.ListSchedules(r.Context(), params)
	if err != nil {
		s.logger.Error("list schedules", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list schedules")
		return
	}

	resp := make([]scheduleResponse, 0, len(schedules))
	var nextPageToken string

	for i, sc := range schedules {
		if i >= pageSize {
			nextPageToken = sc.ID
			break
		}
		resp = append(resp, toScheduleResponse(sc))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"schedules":     resp,
		"nextPageToken": nextPageToken,
	})
}

// AIP-131: GetSchedule
func (s *Server) getSchedule(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("schedule")
	sc, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		s.logger.Error("get schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get schedule")
		return
	}
	if sc == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	if !authorizeScheduleAccess(w, idn, sc) {
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(sc))
}

// AIP-134: UpdateSchedule
func (s *Server) updateSchedule(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("schedule")
	current, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		s.logger.Error("get schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get schedule")
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	if !authorizeScheduleAccess(w, idn, current) {
		return
	}

	var req updateScheduleRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if err := s.store.UpdateSchedule(r.Context(), id, req.Title, req.Description); err != nil {
		s.logger.Error("update schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update schedule")
		return
	}

	sc, err := s.store.GetSchedule(r.Context(), id)
	if err != nil || sc == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(sc))
}

// AIP-135: DeleteSchedule
func (s *Server) deleteSchedule(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("schedule")
	sc, err := s.store.GetSchedule(r.Context(), id)
	if err != nil {
		s.logger.Error("get schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get schedule")
		return
	}
	if sc == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	if !authorizeScheduleAccess(w, idn, sc) {
		return
	}
	if err := s.store.DeleteSchedule(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AIP-136: PauseSchedule
func (s *Server) pauseSchedule(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("schedule")
	sc, err := s.store.GetSchedule(r.Context(), id)
	if err != nil || sc == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	if !authorizeScheduleAccess(w, idn, sc) {
		return
	}
	if sc.State != store.ScheduleActive {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("schedule is not active (state: %s)", sc.State))
		return
	}
	if err := s.store.UpdateScheduleState(r.Context(), id, store.SchedulePaused); err != nil {
		s.logger.Error("pause schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to pause schedule")
		return
	}
	sc, err = s.store.GetSchedule(r.Context(), id)
	if err != nil || sc == nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated schedule")
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(sc))
}

// AIP-136: ResumeSchedule
func (s *Server) resumeSchedule(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("schedule")
	sc, err := s.store.GetSchedule(r.Context(), id)
	if err != nil || sc == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("schedule not found: %s", id))
		return
	}
	if !authorizeScheduleAccess(w, idn, sc) {
		return
	}
	if sc.State != store.SchedulePaused {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("schedule is not paused (state: %s)", sc.State))
		return
	}

	// Recompute next run time from now
	spec, err := schedule.Parse(sc.ScheduleExpr)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "invalid schedule expression in database")
		return
	}
	next := spec.Next(nowUTC())

	if err := s.store.ResumeSchedule(r.Context(), id, next); err != nil {
		s.logger.Error("resume schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to resume schedule")
		return
	}

	sc, err = s.store.GetSchedule(r.Context(), id)
	if err != nil || sc == nil {
		writeError(w, http.StatusInternalServerError, "failed to retrieve updated schedule")
		return
	}
	writeJSON(w, http.StatusOK, toScheduleResponse(sc))
}
