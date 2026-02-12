package workflow

import (
	"context"
	"workflow-code-test/api/services/nodes"
	"workflow-code-test/api/services/storage"
)

type EdgeTarget = edgeTarget

func ExecuteWorkflow(ctx context.Context, wf *storage.Workflow, inputs map[string]any, deps nodes.Deps) (*ExecutionResponse, error) {
	return executeWorkflow(ctx, wf, inputs, deps)
}

func ValidateGraph(storageNodes []storage.Node, adjacency map[string][]edgeTarget) (string, error) {
	return validateGraph(storageNodes, adjacency)
}

func NextNode(edges []edgeTarget, branch string) string {
	return nextNode(edges, branch)
}
