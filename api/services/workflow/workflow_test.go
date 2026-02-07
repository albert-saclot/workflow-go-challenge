package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"

	"workflow-code-test/api/services/storage"
)

// mockStorage implements storage.Storage for testing handlers
// without a real database connection.
type mockStorage struct {
	workflow *storage.Workflow
	err      error
}

func (m *mockStorage) GetWorkflow(_ context.Context, _ uuid.UUID) (*storage.Workflow, error) {
	return m.workflow, m.err
}

// newTestRouter wires up the service with mux routing so handler tests
// can exercise the full request path including URL parameter extraction.
func newTestRouter(svc *Service) *mux.Router {
	router := mux.NewRouter()
	api := router.PathPrefix("/api/v1").Subrouter()
	svc.LoadRoutes(api)
	return router
}

func TestNewService_NilStore(t *testing.T) {
	_, err := NewService(nil)
	if err == nil {
		t.Error("expected error for nil store, got nil")
	}
}

func TestHandleGetWorkflow(t *testing.T) {
	wfID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	sampleWorkflow := &storage.Workflow{
		ID:   wfID,
		Name: "Weather Check System",
		Nodes: []storage.Node{
			{
				ID:       "start",
				Type:     "start",
				Position: storage.NodePosition{X: -160, Y: 300},
				Data: storage.NodeData{
					Label:       "Start",
					Description: "Begin weather check workflow",
					Metadata:    json.RawMessage(`{"hasHandles":{"source":true,"target":false}}`),
				},
			},
		},
		Edges: []storage.Edge{},
	}

	tests := []struct {
		name           string
		url            string
		store          *mockStorage
		wantStatus     int
		checkBody      func(t *testing.T, body []byte)
	}{
		{
			name:       "invalid UUID returns 400",
			url:        "/api/v1/workflows/not-a-uuid",
			store:      &mockStorage{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "workflow not found returns 404",
			url:        "/api/v1/workflows/" + uuid.New().String(),
			store:      &mockStorage{err: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "storage error returns 500",
			url:        "/api/v1/workflows/" + uuid.New().String(),
			store:      &mockStorage{err: errors.New("connection refused")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "valid workflow returns 200 with React Flow shape",
			url:        "/api/v1/workflows/" + wfID.String(),
			store:      &mockStorage{workflow: sampleWorkflow},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(body, &result); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				// ToFrontend() should only return id, nodes, edges
				for _, required := range []string{"id", "nodes", "edges"} {
					if _, ok := result[required]; !ok {
						t.Errorf("response missing required field %q", required)
					}
				}

				// Internal fields should be stripped by ToFrontend()
				for _, excluded := range []string{"name", "createdAt", "modifiedAt"} {
					if _, ok := result[excluded]; ok {
						t.Errorf("response should not contain internal field %q", excluded)
					}
				}

				// Verify node data is present
				var nodes []json.RawMessage
				if err := json.Unmarshal(result["nodes"], &nodes); err != nil {
					t.Fatalf("failed to unmarshal nodes: %v", err)
				}
				if len(nodes) != 1 {
					t.Errorf("expected 1 node, got %d", len(nodes))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewService(tt.store)
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			router := newTestRouter(svc)
			req := httptest.NewRequest(http.MethodGet, tt.url, nil)
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.checkBody != nil {
				tt.checkBody(t, rec.Body.Bytes())
			}
		})
	}
}

func TestHandleExecuteWorkflow(t *testing.T) {
	wfID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	// Minimal workflow: start → end (no external calls needed)
	startEndWorkflow := &storage.Workflow{
		ID:   wfID,
		Name: "Test Workflow",
		Nodes: []storage.Node{
			{
				ID:       "start",
				Type:     "start",
				Position: storage.NodePosition{X: 0, Y: 0},
				Data: storage.NodeData{
					Label:       "Start",
					Description: "Begin workflow",
					Metadata:    json.RawMessage(`{}`),
				},
			},
			{
				ID:       "end",
				Type:     "end",
				Position: storage.NodePosition{X: 100, Y: 0},
				Data: storage.NodeData{
					Label:       "End",
					Description: "End workflow",
					Metadata:    json.RawMessage(`{}`),
				},
			},
		},
		Edges: []storage.Edge{
			{
				ID:     "e-start-end",
				Source: "start",
				Target: "end",
				Type:   "smoothstep",
			},
		},
	}

	tests := []struct {
		name       string
		url        string
		body       string
		store      *mockStorage
		wantStatus int
		checkBody  func(t *testing.T, body []byte)
	}{
		{
			name:       "invalid UUID returns 400",
			url:        "/api/v1/workflows/bad-id/execute",
			body:       `{}`,
			store:      &mockStorage{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty body returns 400",
			url:        "/api/v1/workflows/" + wfID.String() + "/execute",
			body:       "",
			store:      &mockStorage{workflow: startEndWorkflow},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "workflow not found returns 404",
			url:        "/api/v1/workflows/" + uuid.New().String() + "/execute",
			body:       `{}`,
			store:      &mockStorage{err: pgx.ErrNoRows},
			wantStatus: http.StatusNotFound,
		},
		{
			name:       "storage error returns 500",
			url:        "/api/v1/workflows/" + uuid.New().String() + "/execute",
			body:       `{}`,
			store:      &mockStorage{err: errors.New("connection refused")},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "start-end workflow executes successfully",
			url:        "/api/v1/workflows/" + wfID.String() + "/execute",
			body:       `{"formData":{"name":"Alice"},"condition":{}}`,
			store:      &mockStorage{workflow: startEndWorkflow},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var result ExecutionResponse
				if err := json.Unmarshal(body, &result); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				if result.Status != "completed" {
					t.Errorf("expected status 'completed', got %q", result.Status)
				}
				if result.ExecutedAt == "" {
					t.Error("executedAt should not be empty")
				}
				if len(result.Steps) != 2 {
					t.Fatalf("expected 2 steps (start + end), got %d", len(result.Steps))
				}

				// Verify step order
				if result.Steps[0].Type != "start" {
					t.Errorf("first step should be 'start', got %q", result.Steps[0].Type)
				}
				if result.Steps[1].Type != "end" {
					t.Errorf("second step should be 'end', got %q", result.Steps[1].Type)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, err := NewService(tt.store)
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			router := newTestRouter(svc)
			req := httptest.NewRequest(http.MethodPost, tt.url, strings.NewReader(tt.body))
			rec := httptest.NewRecorder()

			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Errorf("expected status %d, got %d (body: %s)", tt.wantStatus, rec.Code, rec.Body.String())
			}

			if tt.checkBody != nil {
				tt.checkBody(t, rec.Body.Bytes())
			}
		})
	}
}
