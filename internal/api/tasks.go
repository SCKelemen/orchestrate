package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/store"
)

// createTaskRequest is the JSON body for CreateTask.
type createTaskRequest struct {
	Agent       string `json:"agent"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
	RepoURL     string `json:"repoUrl"`
	BaseRef     string `json:"baseRef"`
	Strategy    string `json:"strategy"`
	AgentCount  int    `json:"agentCount"`
	Priority    int    `json:"priority"`
	Image       string `json:"image"`
}

// updateTaskRequest is the JSON body for UpdateTask.
type updateTaskRequest struct {
	Title       *string `json:"title"`
	Description *string `json:"description"`
	Priority    *int    `json:"priority"`
}

// taskResponse wraps a task for AIP-style JSON output.
type taskResponse struct {
	Name        string  `json:"name"`
	Agent       string  `json:"agent"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Prompt      string  `json:"prompt"`
	RepoURL     string  `json:"repoUrl"`
	BaseRef     string  `json:"baseRef"`
	Strategy    string  `json:"strategy"`
	AgentCount  int     `json:"agentCount"`
	Priority    int     `json:"priority"`
	State       string  `json:"state"`
	Image       string  `json:"image"`
	CreateTime  string  `json:"createTime"`
	StartTime   *string `json:"startTime"`
	EndTime     *string `json:"endTime"`
}

func toTaskResponse(t *store.Task) taskResponse {
	return taskResponse{
		Name:        "tasks/" + t.ID,
		Agent:       t.Agent,
		Title:       t.Title,
		Description: t.Description,
		Prompt:      t.Prompt,
		RepoURL:     t.RepoURL,
		BaseRef:     t.BaseRef,
		Strategy:    string(t.Strategy),
		AgentCount:  t.AgentCount,
		Priority:    t.Priority,
		State:       string(t.State),
		Image:       t.Image,
		CreateTime:  t.CreateTime,
		StartTime:   t.StartTime,
		EndTime:     t.EndTime,
	}
}

func authorizeTaskAccess(w http.ResponseWriter, id *auth.Identity, t *store.Task) bool {
	if id == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return false
	}
	if isAdminIdentity(id) {
		return true
	}
	if t.OwnerUserID != id.UserID {
		writeError(w, http.StatusForbidden, "forbidden")
		return false
	}
	return true
}

// newID generates a time-sortable random ID (simplified ULID-style).
func newID() string {
	// 6 bytes timestamp (ms) + 10 bytes random = 32 hex chars
	ts := time.Now().UnixMilli()
	b := make([]byte, 16)
	b[0] = byte(ts >> 40)
	b[1] = byte(ts >> 32)
	b[2] = byte(ts >> 24)
	b[3] = byte(ts >> 16)
	b[4] = byte(ts >> 8)
	b[5] = byte(ts)
	if _, err := rand.Read(b[6:]); err != nil {
		// Fall back to timestamp-derived bytes if CSPRNG is unavailable.
		ns := time.Now().UnixNano()
		for i := 6; i < len(b); i++ {
			shift := uint((i - 6) * 8)
			b[i] = byte(ns >> shift)
		}
	}
	return hex.EncodeToString(b)
}

// AIP-131: CreateTask
func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	var req createTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
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

	id := newID()
	task, err := s.store.CreateTask(r.Context(), id, store.CreateTaskParams{
		OwnerUserID: idn.UserID,
		Agent:       agentBackend,
		Title:       req.Title,
		Description: req.Description,
		Prompt:      req.Prompt,
		RepoURL:     req.RepoURL,
		BaseRef:     req.BaseRef,
		Strategy:    strategy,
		AgentCount:  agentCount,
		Priority:    req.Priority,
		Image:       req.Image,
	})
	if err != nil {
		s.logger.Error("create task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to create task")
		return
	}
	writeJSON(w, http.StatusCreated, toTaskResponse(task))
}

// AIP-132: ListTasks
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
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

	params := store.ListTasksParams{
		State:     store.TaskState(q.Get("state")),
		PageSize:  pageSize,
		PageToken: q.Get("pageToken"),
	}
	if !isAdminIdentity(idn) {
		params.OwnerUserID = idn.UserID
	}

	tasks, err := s.store.ListTasks(r.Context(), params)
	if err != nil {
		s.logger.Error("list tasks", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list tasks")
		return
	}

	resp := make([]taskResponse, 0, len(tasks))
	var nextPageToken string

	for i, t := range tasks {
		if i >= pageSize {
			nextPageToken = t.ID
			break
		}
		resp = append(resp, toTaskResponse(t))
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"tasks":         resp,
		"nextPageToken": nextPageToken,
	})
}

// AIP-131: GetTask
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("task")
	task, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		s.logger.Error("get task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// AIP-134: UpdateTask
func (s *Server) updateTask(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("task")
	current, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		s.logger.Error("get task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	if current == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	if !authorizeTaskAccess(w, idn, current) {
		return
	}

	var req updateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	if err := s.store.UpdateTask(r.Context(), id, req.Title, req.Description, req.Priority); err != nil {
		s.logger.Error("update task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to update task")
		return
	}

	task, err := s.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// AIP-135: DeleteTask
func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("task")
	task, err := s.store.GetTask(r.Context(), id)
	if err != nil {
		s.logger.Error("get task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to get task")
		return
	}
	if task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}
	if err := s.store.DeleteTask(r.Context(), id); err != nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// AIP-136: CancelTask
func (s *Server) cancelTask(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("task")
	task, err := s.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}
	if task.State != store.TaskQueued && task.State != store.TaskRunning {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot cancel task in state %s", task.State))
		return
	}
	if err := s.store.UpdateTaskState(r.Context(), id, store.TaskCancelled); err != nil {
		s.logger.Error("cancel task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to cancel task")
		return
	}
	task, _ = s.store.GetTask(r.Context(), id)
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}

// AIP-136: RetryTask
func (s *Server) retryTask(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	id := r.PathValue("task")
	task, err := s.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", id))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}
	if task.State != store.TaskFailed && task.State != store.TaskCancelled {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("cannot retry task in state %s", task.State))
		return
	}
	if err := s.store.UpdateTaskState(r.Context(), id, store.TaskQueued); err != nil {
		s.logger.Error("retry task", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to retry task")
		return
	}
	task, _ = s.store.GetTask(r.Context(), id)
	writeJSON(w, http.StatusOK, toTaskResponse(task))
}
