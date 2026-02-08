package storagemock

import (
	"context"
	"encoding/json"
	"workflow-code-test/api/services/storage"

	"github.com/google/uuid"
)

type StorageMock struct {
	GetWorkflowMock    func(ctx context.Context, id uuid.UUID) (*storage.Workflow, error)
	UpsertWorkflowMock func(ctx context.Context, wf *storage.Workflow) error
	DeleteWorkflowMock func(ctx context.Context, id uuid.UUID) error
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
