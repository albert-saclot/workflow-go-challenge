package workflow_test

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

	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"
	"workflow-code-test/api/services/storage/storagemock"
	"workflow-code-test/api/services/workflow"
)

// newTestRouter wires up the service with mux routing so handler tests
// can exercise the full request path including URL parameter extraction.
func newTestRouter(svc *workflow.Service) *mux.Router {
	router := mux.NewRouter()
	api := router.PathPrefix("/api/v1").Subrouter()
	svc.LoadRoutes(api)
	return router
}

func TestNewService_NilStore(t *testing.T) {
	t.Parallel()
	_, err := workflow.NewService(nil, nodes.Deps{})
	if err == nil {
		t.Error("expected error for nil store, got nil")
	}
}

func TestHandleGetWorkflow(t *testing.T) {
	t.Parallel()

	wfUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	tests := [...]struct {
		name       string
		url        string
		store      *storagemock.StorageMock
		wantStatus int
		checkBody  func(t *testing.T, body []byte)
	}{
		{
			name:       "invalid UUID returns 400",
			url:        "/api/v1/workflows/not-a-uuid",
			store:      &storagemock.StorageMock{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "workflow not found returns 404",
			url:  "/api/v1/workflows/" + wfUUID.String(),
			store: &storagemock.StorageMock{
				GetWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error) {
					return nil, pgx.ErrNoRows
				},
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "storage error returns 500",
			url:  "/api/v1/workflows/" + uuid.New().String(),
			store: &storagemock.StorageMock{
				GetWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error) {
					return nil, errors.New("connection refused")
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "valid workflow returns 200 with React Flow shape",
			url:        "/api/v1/workflows/" + wfUUID.String(),
			store:      &storagemock.StorageMock{},
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
			t.Parallel()
			svc, err := workflow.NewService(tt.store, nodes.Deps{})
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
	t.Parallel()

	// Minimal workflow: start → end (no external calls needed)
	wfUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	startEndWorkflow := &storage.Workflow{
		ID:   wfUUID,
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

	tests := [...]struct {
		name       string
		url        string
		body       string
		store      *storagemock.StorageMock
		wantStatus int
		checkBody  func(t *testing.T, body []byte)
	}{
		{
			name:       "invalid UUID returns 400",
			url:        "/api/v1/workflows/bad-id/execute",
			body:       `{}`,
			store:      &storagemock.StorageMock{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "empty body returns 400",
			url:        "/api/v1/workflows/" + wfUUID.String() + "/execute",
			body:       "",
			store:      &storagemock.StorageMock{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "workflow not found returns 404",
			url:  "/api/v1/workflows/" + uuid.New().String() + "/execute",
			body: `{}`,
			store: &storagemock.StorageMock{
				GetWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error) {
					return nil, pgx.ErrNoRows
				},
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "storage error returns 500",
			url:  "/api/v1/workflows/" + uuid.New().String() + "/execute",
			body: `{}`,
			store: &storagemock.StorageMock{
				GetWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error) {
					return nil, errors.New("connection refused")
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name: "start-end workflow executes successfully",
			url:  "/api/v1/workflows/" + wfUUID.String() + "/execute",
			body: `{"formData":{"name":"Alice"},"condition":{}}`,
			store: &storagemock.StorageMock{
				GetWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error) {
					return startEndWorkflow, nil
				},
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var result workflow.ExecutionResponse
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
		{
			name: "executes from snapshot when available",
			url:  "/api/v1/workflows/" + wfUUID.String() + "/execute",
			body: `{"formData":{"name":"Alice"},"condition":{}}`,
			store: &storagemock.StorageMock{
				GetActiveSnapshotMock: func(ctx context.Context, workflowID uuid.UUID) (*storage.WorkflowSnapshot, error) {
					return &storage.WorkflowSnapshot{
						ID:            uuid.New(),
						WorkflowID:    workflowID,
						VersionNumber: 1,
						DagData: storage.DagData{
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
						},
					}, nil
				},
				GetWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error) {
					t.Error("GetWorkflow should not be called when snapshot is available")
					return nil, errors.New("should not be called")
				},
			},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var result workflow.ExecutionResponse
				if err := json.Unmarshal(body, &result); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				if result.Status != "completed" {
					t.Errorf("expected status 'completed', got %q", result.Status)
				}
				if len(result.Steps) != 2 {
					t.Fatalf("expected 2 steps (start + end), got %d", len(result.Steps))
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, err := workflow.NewService(tt.store, nodes.Deps{})
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

func TestHandlePublishWorkflow(t *testing.T) {
	t.Parallel()

	wfUUID := uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")

	tests := [...]struct {
		name       string
		url        string
		store      *storagemock.StorageMock
		wantStatus int
		checkBody  func(t *testing.T, body []byte)
	}{
		{
			name:       "invalid UUID returns 400",
			url:        "/api/v1/workflows/bad-id/publish",
			store:      &storagemock.StorageMock{},
			wantStatus: http.StatusBadRequest,
		},
		{
			name: "workflow not found returns 404",
			url:  "/api/v1/workflows/" + wfUUID.String() + "/publish",
			store: &storagemock.StorageMock{
				PublishWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.WorkflowSnapshot, error) {
					return nil, pgx.ErrNoRows
				},
			},
			wantStatus: http.StatusNotFound,
		},
		{
			name: "storage error returns 500",
			url:  "/api/v1/workflows/" + wfUUID.String() + "/publish",
			store: &storagemock.StorageMock{
				PublishWorkflowMock: func(ctx context.Context, id uuid.UUID) (*storage.WorkflowSnapshot, error) {
					return nil, errors.New("connection refused")
				},
			},
			wantStatus: http.StatusInternalServerError,
		},
		{
			name:       "successful publish returns 200 with snapshot info",
			url:        "/api/v1/workflows/" + wfUUID.String() + "/publish",
			store:      &storagemock.StorageMock{},
			wantStatus: http.StatusOK,
			checkBody: func(t *testing.T, body []byte) {
				var result map[string]json.RawMessage
				if err := json.Unmarshal(body, &result); err != nil {
					t.Fatalf("failed to unmarshal response: %v", err)
				}

				for _, required := range []string{"snapshotId", "versionNumber", "publishedAt"} {
					if _, ok := result[required]; !ok {
						t.Errorf("response missing required field %q", required)
					}
				}

				var version int
				if err := json.Unmarshal(result["versionNumber"], &version); err != nil {
					t.Fatalf("failed to unmarshal versionNumber: %v", err)
				}
				if version != 1 {
					t.Errorf("expected version 1, got %d", version)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			svc, err := workflow.NewService(tt.store, nodes.Deps{})
			if err != nil {
				t.Fatalf("failed to create service: %v", err)
			}

			router := newTestRouter(svc)
			req := httptest.NewRequest(http.MethodPost, tt.url, nil)
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
