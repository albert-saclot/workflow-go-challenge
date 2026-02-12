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

	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"
)

// maxRequestBody limits the size of the execute request body to prevent abuse.
const maxRequestBody = 1 << 20 // 1MB

// HandleGetWorkflow loads a workflow definition by ID from the database and
// returns it as JSON in the format React Flow expects (id, nodes, edges).
func (s *Service) HandleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	rid := reqID(r)
	id := mux.Vars(r)["id"]
	slog.Debug("returning workflow definition", "id", id, "requestId", rid)

	wfUUID, err := uuid.Parse(id)
	if err != nil {
		slog.Warn("invalid workflow id", "id", id, "requestId", rid, "error", err)
		writeErrorJSON(w, "INVALID_ID", "invalid workflow id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	wf, err := s.storage.GetWorkflow(ctx, wfUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("workflow not found", "id", wfUUID, "requestId", rid)
			writeErrorJSON(w, "NOT_FOUND", "workflow not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to get workflow", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	nodeJSONs, err := buildNodeJSONs(wf.Nodes, s.deps)
	if err != nil {
		slog.Error("failed to construct nodes", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	payload, err := json.Marshal(map[string]any{
		"id":    wf.ID,
		"nodes": nodeJSONs,
		"edges": wf.Edges,
	})
	if err != nil {
		slog.Error("failed to marshal workflow", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(payload); err != nil {
		slog.Error("failed to write response", "id", wfUUID, "requestId", rid, "error", err)
	}
}

// HandlePublishWorkflow creates an immutable snapshot of the workflow's current
// DAG. Subsequent executions will run against this frozen snapshot rather than
// live tables, decoupling execution from node_library mutations.
func (s *Service) HandlePublishWorkflow(w http.ResponseWriter, r *http.Request) {
	rid := reqID(r)
	id := mux.Vars(r)["id"]
	slog.Debug("publishing workflow", "id", id, "requestId", rid)

	wfUUID, err := uuid.Parse(id)
	if err != nil {
		slog.Warn("invalid workflow id", "id", id, "requestId", rid, "error", err)
		writeErrorJSON(w, "INVALID_ID", "invalid workflow id", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	snap, err := s.storage.PublishWorkflow(ctx, wfUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			slog.Warn("workflow not found for publish", "id", wfUUID, "requestId", rid)
			writeErrorJSON(w, "NOT_FOUND", "workflow not found", http.StatusNotFound)
			return
		}
		slog.Error("failed to publish workflow", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	payload, err := json.Marshal(map[string]any{
		"snapshotId":    snap.ID,
		"versionNumber": snap.VersionNumber,
		"publishedAt":   snap.PublishedAt,
	})
	if err != nil {
		slog.Error("failed to marshal publish response", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(payload); err != nil {
		slog.Error("failed to write response", "id", wfUUID, "requestId", rid, "error", err)
	}
}

// HandleExecuteWorkflow loads a workflow from the database, parses the input
// variables from the request body, and executes the workflow graph end-to-end.
// If the workflow has a published snapshot, execution runs against the frozen
// snapshot. Otherwise it falls back to live tables (backward compat for drafts).
// Execution failures (node errors, cycles) are returned as 200 with
// status "failed" and partial results — they are business-level outcomes,
// not server errors.
func (s *Service) HandleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	rid := reqID(r)
	id := mux.Vars(r)["id"]
	slog.Debug("handling workflow execution", "id", id, "requestId", rid)

	wfUUID, err := uuid.Parse(id)
	if err != nil {
		slog.Warn("invalid workflow id", "id", id, "requestId", rid, "error", err)
		writeErrorJSON(w, "INVALID_ID", "invalid workflow id", http.StatusBadRequest)
		return
	}

	// Limit request body size to prevent abuse.
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)

	// Parse the request body. The frontend sends:
	//   { "formData": { "name": ..., "city": ... }, "condition": { "operator": ..., "threshold": ... } }
	// We flatten both into a single variables map for the engine.
	var body struct {
		FormData  map[string]any `json:"formData"`
		Condition map[string]any `json:"condition"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		slog.Warn("failed to decode request body", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INVALID_BODY", "invalid request body", http.StatusBadRequest)
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

	// Prefer executing from a published snapshot if one exists.
	// This decouples execution from live node_library mutations.
	var wf *storage.Workflow
	snapshot, err := s.storage.GetActiveSnapshot(ctx, wfUUID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		slog.Error("failed to get active snapshot", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	if snapshot != nil {
		slog.Debug("executing from snapshot", "id", wfUUID, "version", snapshot.VersionNumber, "requestId", rid)
		wf = &storage.Workflow{
			ID:    wfUUID,
			Nodes: snapshot.DagData.Nodes,
			Edges: snapshot.DagData.Edges,
		}
	} else {
		// No snapshot — fall back to live tables (backward compat for drafts)
		wf, err = s.storage.GetWorkflow(ctx, wfUUID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				slog.Warn("workflow not found", "id", wfUUID, "requestId", rid)
				writeErrorJSON(w, "NOT_FOUND", "workflow not found", http.StatusNotFound)
				return
			}
			slog.Error("failed to get workflow", "id", wfUUID, "requestId", rid, "error", err)
			writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
			return
		}
	}

	executedAt := time.Now().Format(time.RFC3339)
	result, err := executeWorkflow(ctx, wf, inputs, s.deps)
	if err != nil {
		// Hard errors (e.g. invalid node metadata) are server-level failures
		slog.Error("workflow execution failed", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}
	result.ExecutedAt = executedAt

	if result.Status == "failed" {
		slog.Warn("workflow completed with failure",
			"id", wfUUID,
			"requestId", rid,
			"failedNode", result.FailedNode,
			"error", result.Error,
		)
	}

	payload, err := json.Marshal(result)
	if err != nil {
		slog.Error("failed to marshal execution result", "id", wfUUID, "requestId", rid, "error", err)
		writeErrorJSON(w, "INTERNAL_ERROR", "internal server error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(payload); err != nil {
		slog.Error("failed to write response", "id", wfUUID, "requestId", rid, "error", err)
	}
}

// buildNodeJSONs constructs typed nodes from storage data and calls
// each node's ToJSON() to produce the frontend representation.
func buildNodeJSONs(storageNodes []storage.Node, deps nodes.Deps) ([]nodes.NodeJSON, error) {
	result := make([]nodes.NodeJSON, 0, len(storageNodes))
	for _, sn := range storageNodes {
		base := nodes.BaseFields{
			ID:          sn.ID,
			NodeType:    sn.Type,
			Position:    nodes.Position{X: sn.Position.X, Y: sn.Position.Y},
			Label:       sn.Data.Label,
			Description: sn.Data.Description,
			Metadata:    sn.Data.Metadata,
		}

		n, err := nodes.New(base, deps)
		if err != nil {
			return nil, fmt.Errorf("node %q: %w", sn.ID, err)
		}
		result = append(result, n.ToJSON())
	}
	return result, nil
}

// writeErrorJSON writes a structured JSON error response with a machine-readable
// code and a human-readable message. The code allows clients to programmatically
// distinguish between error types (e.g. retry on INTERNAL_ERROR, don't retry on NOT_FOUND).
func writeErrorJSON(w http.ResponseWriter, errCode, message string, status int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{"code": errCode, "message": message})
}

// reqID extracts the request ID from context (set by requestIDMiddleware).
func reqID(r *http.Request) string {
	id, _ := r.Context().Value(requestIDKey).(string)
	return id
}
