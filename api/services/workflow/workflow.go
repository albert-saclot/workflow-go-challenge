package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

// HandleGetWorkflow loads a workflow definition by ID from the database and
// returns it as JSON in the format React Flow expects (id, nodes, edges).
func (s *Service) HandleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	slog.Debug("Returning workflow definition for id", "id", id)

	wfUUID, err := uuid.Parse(id)
	if err != nil {
		slog.Error("invalid workflow id provided", "error", err)
		http.Error(w, "invalid workflow id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	wf, err := s.storage.GetWorkflow(ctx, wfUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Error("workflow not found", "error", wfUUID)
			http.Error(w, "workflow not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get workflow", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	payload, err := json.Marshal(wf.ToFrontend())
	if err != nil {
		slog.Error("failed to marshal workflow", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(payload)
}

// HandleExecuteWorkflow loads a workflow from the database, parses the input
// variables from the request body, and executes the workflow graph end-to-end.
func (s *Service) HandleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	slog.Debug("handling workflow execution", "id", id)

	wfUUID, err := uuid.Parse(id)
	if err != nil {
		slog.Error("invalid workflow id provided", "error", err)
		http.Error(w, "invalid workflow id", http.StatusBadRequest)
		return
	}

	// Parse the request body. The frontend sends:
	//   { "formData": { "name": ..., "city": ... }, "condition": { "operator": ..., "threshold": ... } }
	// We flatten both into a single variables map for the engine.
	var body struct {
		FormData  map[string]any `json:"formData"`
		Condition map[string]any `json:"condition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Error("failed to decode request body", "error", err)
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	inputs := make(map[string]any)
	for k, v := range body.FormData {
		inputs[k] = v
	}
	for k, v := range body.Condition {
		inputs[k] = v
	}

	ctx := r.Context()
	wf, err := s.storage.GetWorkflow(ctx, wfUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Error("workflow not found", "id", wfUUID)
			http.Error(w, "workflow not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get workflow", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	executedAt := time.Now().Format(time.RFC3339)
	result, err := executeWorkflow(ctx, wf, inputs, s.deps)
	if err != nil {
		slog.Error("workflow execution failed", "error", err)
		http.Error(w, fmt.Sprintf("execution failed: %s", err.Error()), http.StatusInternalServerError)
		return
	}
	result.ExecutedAt = executedAt

	payload, err := json.Marshal(result)
	if err != nil {
		slog.Error("failed to marshal execution result", "error", err)
		http.Error(w, "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	w.Write(payload)
}
