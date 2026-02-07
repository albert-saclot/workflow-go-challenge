package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DB abstracts the database operations used by the storage layer.
// Satisfied by *pgxpool.Pool in production and pgxmock in tests.
type DB interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// pgStorage implements the Storage interface using PostgreSQL.
type pgStorage struct {
	db DB
}

// Storage defines the interface for workflow data access.
// This abstraction allows the workflow service to remain decoupled from
// the persistence layer, making it testable and swappable.
type Storage interface {
	GetWorkflow(ctx context.Context, id uuid.UUID) (*Workflow, error)
}

// NewInstance creates a new PostgreSQL-backed Storage implementation.
func NewInstance(db *pgxpool.Pool) (Storage, error) {
	if db == nil {
		return nil, fmt.Errorf("repository: db connection cannot be nil")
	}
	return &pgStorage{db: db}, nil
}

// GetWorkflow retrieves a complete workflow by ID, hydrating it from three tables:
//   - workflows: the container (name, timestamps)
//   - workflow_node_instances + node_library: canvas positions joined with reusable blueprints
//   - workflow_edges: directed connections between node instances
//
// The result is a fully assembled Workflow ready for JSON serialization to React Flow.
func (r *pgStorage) GetWorkflow(ctx context.Context, id uuid.UUID) (*Workflow, error) {

	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	wf := &Workflow{
		ID:    id,
		Nodes: []Node{},
		Edges: []Edge{},
	}

	// 1. Fetch workflow header, respecting soft-deletion.
	err := r.db.QueryRow(timeoutCtx, `
        SELECT name, created_at, modified_at
        FROM workflows
        WHERE id = $1 AND deleted_at IS NULL`,
		id).Scan(&wf.Name, &wf.CreatedAt, &wf.ModifiedAt)

	if err != nil {
		return nil, err // pgx.ErrNoRows if not found
	}

	// 2. Hydrate nodes by joining instance positions with library blueprints.
	// Each node instance on the canvas references a node_library entry that holds
	// the reusable logic (type, label, description, metadata).
	nodeRows, err := r.db.Query(timeoutCtx, `
        SELECT
            i.instance_id,
            l.node_type,
            i.x_pos, i.y_pos,
            l.base_label as label,
            l.base_description,
            l.metadata
        FROM workflow_node_instances i
        JOIN node_library l ON i.node_library_id = l.id
        WHERE i.workflow_id = $1 AND l.deleted_at IS NULL`,
		id)
	if err != nil {
		return nil, err
	}
	defer nodeRows.Close()

	for nodeRows.Next() {
		var n Node
		err := nodeRows.Scan(
			&n.ID,
			&n.Type,
			&n.Position.X, &n.Position.Y,
			&n.Data.Label,
			&n.Data.Description,
			&n.Data.Metadata,
		)
		if err != nil {
			return nil, err
		}
		wf.Nodes = append(wf.Nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, err
	}

	// 3. Fetch edges with their visual properties (animation, labels, styling).
	// source_handle is used by condition nodes to distinguish true/false branches.
	edgeRows, err := r.db.Query(timeoutCtx, `
        SELECT edge_id, source_instance_id, target_instance_id, source_handle,
               edge_type, animated, label, style_props, label_style
        FROM workflow_edges
        WHERE workflow_id = $1`,
		id)
	if err != nil {
		return nil, err
	}
	defer edgeRows.Close()

	for edgeRows.Next() {
		var e Edge
		err := edgeRows.Scan(
			&e.ID,
			&e.Source,
			&e.Target,
			&e.SourceHandle,
			&e.Type,
			&e.Animated,
			&e.Label,
			&e.Style,
			&e.LabelStyle,
		)
		if err != nil {
			return nil, err
		}
		wf.Edges = append(wf.Edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return nil, err
	}

	return wf, nil
}
