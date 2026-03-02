package api

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

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

	logPath, err := s.resolveRunLogPath(run)
	if err != nil {
		writeError(w, http.StatusNotFound, "log file not found")
		return
	}

	// #nosec G304,G703 -- logPath is constrained by resolveRunLogPath (run-id filename under logs root).
	f, err := os.Open(logPath)
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
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024) // 512KB max line
	const maxBytes = 10 << 20                          // 10MB
	var streamed int64

	for scanner.Scan() {
		if r.Context().Err() != nil {
			break
		}
		line := sanitizeSSEData(scanner.Text())
		n, _ := fmt.Fprintf(w, "data: %s\n\n", line) // #nosec G705 -- line is sanitized by sanitizeSSEData
		streamed += int64(n)
		flusher.Flush()
		if streamed >= maxBytes {
			fmt.Fprintf(w, "event: truncated\ndata: log size limit reached\n\n")
			flusher.Flush()
			break
		}
	}

	if streamed < maxBytes && r.Context().Err() == nil {
		fmt.Fprintf(w, "event: done\ndata: end of log\n\n")
		flusher.Flush()
	}
}

func (s *Server) resolveRunLogPath(run *store.Run) (string, error) {
	raw := strings.TrimSpace(run.LogPath)
	if raw == "" {
		return "", fmt.Errorf("empty log path")
	}

	cleaned := filepath.Clean(raw)
	if filepath.Base(cleaned) != run.ID+".log" {
		return "", fmt.Errorf("unexpected log file name")
	}

	if strings.TrimSpace(s.logsDir) == "" {
		return cleaned, nil
	}

	root, err := filepath.Abs(s.logsDir)
	if err != nil {
		return "", err
	}
	candidate, err := filepath.Abs(cleaned)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("log path outside allowed root")
	}
	return candidate, nil
}

func sanitizeSSEData(line string) string {
	line = strings.ReplaceAll(line, "\r", "")
	line = strings.ReplaceAll(line, "\n", "")
	return line
}
