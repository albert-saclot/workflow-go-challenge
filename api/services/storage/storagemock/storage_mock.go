package storagemock

import (
	"context"
	"encoding/json"
	"time"
	"workflow-code-test/api/services/storage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type StorageMock struct {
	GetWorkflowMock      func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error)
	UpsertWorkflowMock   func(ctx context.Context, wf *storage.Workflow) error
	DeleteWorkflowMock   func(ctx context.Context, id uuid.UUID) error
	PublishWorkflowMock  func(ctx context.Context, id uuid.UUID) (*storage.WorkflowSnapshot, error)
	GetActiveSnapshotMock func(ctx context.Context, workflowID uuid.UUID) (*storage.WorkflowSnapshot, error)
}

func (m *StorageMock) GetWorkflow(ctx context.Context, wfUUID uuid.UUID) (*storage.Workflow, error) {
	if m != nil && m.GetWorkflowMock != nil {
		return m.GetWorkflowMock(ctx, wfUUID)
	}

	return &storage.Workflow{
		ID:   wfUUID,
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
	}, nil
}

func (m *StorageMock) UpsertWorkflow(ctx context.Context, wf *storage.Workflow) error {
	if m != nil && m.UpsertWorkflowMock != nil {
		return m.UpsertWorkflowMock(ctx, wf)
	}
	return nil
}

func (m *StorageMock) DeleteWorkflow(ctx context.Context, wfUUID uuid.UUID) error {
	if m != nil && m.DeleteWorkflowMock != nil {
		return m.DeleteWorkflowMock(ctx, wfUUID)
	}
	return nil
}

func (m *StorageMock) PublishWorkflow(ctx context.Context, id uuid.UUID) (*storage.WorkflowSnapshot, error) {
	if m != nil && m.PublishWorkflowMock != nil {
		return m.PublishWorkflowMock(ctx, id)
	}
	snapID := uuid.New()
	return &storage.WorkflowSnapshot{
		ID:            snapID,
		WorkflowID:    id,
		VersionNumber: 1,
		DagData:       storage.DagData{Nodes: []storage.Node{}, Edges: []storage.Edge{}},
		PublishedAt:   time.Now(),
	}, nil
}

func (m *StorageMock) GetActiveSnapshot(ctx context.Context, workflowID uuid.UUID) (*storage.WorkflowSnapshot, error) {
	if m != nil && m.GetActiveSnapshotMock != nil {
		return m.GetActiveSnapshotMock(ctx, workflowID)
	}
	// Default: no snapshot (draft workflow) — existing execute tests fall through to GetWorkflow
	return nil, pgx.ErrNoRows
}
