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

// TODO: Update this
func (s *Service) HandleExecuteWorkflow(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	slog.Debug("Handling workflow execution for id", "id", id)

	// Generate current timestamp
	currentTime := time.Now().Format(time.RFC3339)

	executionJSON := fmt.Sprintf(`{
		"executedAt": "%s",
		"status": "completed",
		"steps": [
			{
				"nodeId": "start",
				"type": "start",
				"label": "Start",
				"description": "Begin weather check workflow",
				"status": "completed"
			},
			{
				"nodeId": "form",
				"type": "form",
				"label": "User Input",
				"description": "Process collected data - name, email, location",
				"status": "completed",
				"output": {
					"name": "Alice",
					"email": "alice@example.com",
					"city": "Sydney"
				}
			},
			{
				"nodeId": "weather-api",
				"type": "integration",
				"label": "Weather API",
				"description": "Fetch current temperature for Sydney",
				"status": "completed",
				"output": {
					"temperature": 28.5,
					"location": "Sydney"
				}
			},
			{
				"nodeId": "condition",
				"type": "condition",
				"label": "Check Condition",
				"description": "Evaluate temperature threshold",
				"status": "completed",
				"output": {
					"conditionMet": true,
					"threshold": 25,
					"operator": "greater_than",
					"actualValue": 28.5,
					"message": "Temperature 28.5°C is greater than 25°C - condition met"
				}
			},
			{
				"nodeId": "email",
				"type": "email",
				"label": "Send Alert",
				"description": "Email weather alert notification",
				"status": "completed",
				"output": {
					"emailDraft": {
						"to": "alice@example.com",
						"from": "weather-alerts@example.com",
						"subject": "Weather Alert",
						"body": "Weather alert for Sydney! Temperature is 28.5°C!",
						"timestamp": "2024-01-15T14:30:24.856Z"
					},
					"deliveryStatus": "sent",
					"messageId": "msg_abc123def456",
					"emailSent": true
				}
			},
			{
				"nodeId": "end",
				"type": "end",
				"label": "Complete",
				"description": "Workflow execution finished",
				"status": "completed"
			}
		]
	}`, currentTime)

	w.WriteHeader(http.StatusOK)
	w.Write([]byte(executionJSON))
}
