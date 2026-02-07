package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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
	svc, _ := NewService(&mockStorage{})
	router := newTestRouter(svc)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/"+id.String()+"/execute", nil)
	rec := httptest.NewRecorder()

	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rec.Code)
	}

	// Verify the response is valid JSON with expected fields
	var result map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}

	if _, ok := result["executedAt"]; !ok {
		t.Error("response missing 'executedAt' field")
	}
	if _, ok := result["status"]; !ok {
		t.Error("response missing 'status' field")
	}
	if _, ok := result["steps"]; !ok {
		t.Error("response missing 'steps' field")
	}
}
