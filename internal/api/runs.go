package api

import (
	"bufio"
	"fmt"
	"net/http"
	"os"

	"github.com/SCKelemen/orchestrate/internal/auth"
	"github.com/SCKelemen/orchestrate/internal/store"
)

type runResponse struct {
	Name       string  `json:"name"`
	AgentIndex int     `json:"agentIndex"`
	Branch     string  `json:"branch"`
	State      string  `json:"state"`
	ExitCode   *int    `json:"exitCode"`
	Output     string  `json:"output"`
	CreateTime string  `json:"createTime"`
	StartTime  *string `json:"startTime"`
	EndTime    *string `json:"endTime"`
}

func toRunResponse(taskID string, r *store.Run) runResponse {
	return runResponse{
		Name:       fmt.Sprintf("tasks/%s/runs/%s", taskID, r.ID),
		AgentIndex: r.AgentIndex,
		Branch:     r.Branch,
		State:      string(r.State),
		ExitCode:   r.ExitCode,
		Output:     r.Output,
		CreateTime: r.CreateTime,
		StartTime:  r.StartTime,
		EndTime:    r.EndTime,
	}
}

func (s *Server) listRuns(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	taskID := r.PathValue("task")

	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}

	runs, err := s.store.ListRuns(r.Context(), taskID)
	if err != nil {
		s.logger.Error("list runs", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list runs")
		return
	}

	resp := make([]runResponse, 0, len(runs))
	for _, run := range runs {
		resp = append(resp, toRunResponse(taskID, run))
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": resp})
}

func (s *Server) getRun(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	taskID := r.PathValue("task")
	runID := r.PathValue("run")

	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}

	run, err := s.store.GetRun(r.Context(), runID)
	if err != nil || run == nil || run.TaskID != taskID {
		writeError(w, http.StatusNotFound, fmt.Sprintf("run not found: %s", runID))
		return
	}
	writeJSON(w, http.StatusOK, toRunResponse(taskID, run))
}

// streamLogs streams agent output as server-sent events.
func (s *Server) streamLogs(w http.ResponseWriter, r *http.Request) {
	idn := auth.FromContext(r.Context())
	if idn == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	taskID := r.PathValue("task")
	runID := r.PathValue("run")

	task, err := s.store.GetTask(r.Context(), taskID)
	if err != nil || task == nil {
		writeError(w, http.StatusNotFound, fmt.Sprintf("task not found: %s", taskID))
		return
	}
	if !authorizeTaskAccess(w, idn, task) {
		return
	}

	run, err := s.store.GetRun(r.Context(), runID)
	if err != nil || run == nil || run.TaskID != taskID {
		writeError(w, http.StatusNotFound, fmt.Sprintf("run not found: %s", runID))
		return
	}

	if run.LogPath == "" {
		writeError(w, http.StatusNotFound, "no log file for this run")
		return
	}

	f, err := os.Open(run.LogPath)
	if err != nil {
		writeError(w, http.StatusNotFound, "log file not found")
		return
	}
	defer f.Close()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fmt.Fprintf(w, "data: %s\n\n", scanner.Text())
		flusher.Flush()
	}

	// If the run is still going, we could tail. For now, just send what we have.
	fmt.Fprintf(w, "event: done\ndata: end of log\n\n")
	flusher.Flush()
}
