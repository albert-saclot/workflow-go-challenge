package storage

import (
	"context"
	"encoding/json"
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
	BeginTx(ctx context.Context, txOptions pgx.TxOptions) (pgx.Tx, error)
}

// querier is satisfied by both pgx.Tx and pgxpool.Pool, allowing
// hydration helpers to work inside or outside transactions.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// pgStorage implements the Storage interface using PostgreSQL.
type pgStorage struct {
	DB DB
}

// Storage defines the interface for workflow data access.
// This abstraction allows the workflow service to remain decoupled from
// the persistence layer, making it testable and swappable.
type Storage interface {
	GetWorkflow(ctx context.Context, id uuid.UUID) (*Workflow, error)
	UpsertWorkflow(ctx context.Context, wf *Workflow) error
	DeleteWorkflow(ctx context.Context, id uuid.UUID) error
	PublishWorkflow(ctx context.Context, id uuid.UUID) (*WorkflowSnapshot, error)
	GetActiveSnapshot(ctx context.Context, workflowID uuid.UUID) (*WorkflowSnapshot, error)
}

// NewInstance creates a new PostgreSQL-backed Storage implementation.
func NewInstance(db *pgxpool.Pool) (Storage, error) {
	if db == nil {
		return nil, fmt.Errorf("repository: db connection cannot be nil")
	}
	return &pgStorage{DB: db}, nil
}

// hydrateNodes fetches workflow nodes by joining instance positions with library blueprints.
func hydrateNodes(ctx context.Context, q querier, workflowID uuid.UUID) ([]Node, error) {
	rows, err := q.Query(ctx, `
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
		workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var n Node
		err := rows.Scan(
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
		nodes = append(nodes, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return nodes, nil
}

// hydrateEdges fetches workflow edges with their visual properties.
func hydrateEdges(ctx context.Context, q querier, workflowID uuid.UUID) ([]Edge, error) {
	rows, err := q.Query(ctx, `
        SELECT edge_id, source_instance_id, target_instance_id, source_handle,
               edge_type, animated, label, style_props, label_style
        FROM workflow_edges
        WHERE workflow_id = $1`,
		workflowID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var edges []Edge
	for rows.Next() {
		var e Edge
		err := rows.Scan(
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
		edges = append(edges, e)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return edges, nil
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

	// Wrap all queries in a read-only transaction so the three SELECTs
	// (header, nodes, edges) see a consistent snapshot of the database.
	tx, err := r.DB.BeginTx(timeoutCtx, pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(timeoutCtx)

	wf := &Workflow{
		ID:    id,
		Nodes: []Node{},
		Edges: []Edge{},
	}

	// 1. Fetch workflow header, respecting soft-deletion.
	err = tx.QueryRow(timeoutCtx, `
        SELECT name, status, active_snapshot_id, created_at, modified_at
        FROM workflows
        WHERE id = $1 AND deleted_at IS NULL`,
		id).Scan(&wf.Name, &wf.Status, &wf.ActiveSnapshotID, &wf.CreatedAt, &wf.ModifiedAt)

	if err != nil {
		return nil, err // pgx.ErrNoRows if not found
	}

	// 2. Hydrate nodes by joining instance positions with library blueprints.
	nodes, err := hydrateNodes(timeoutCtx, tx, id)
	if err != nil {
		return nil, err
	}
	if nodes != nil {
		wf.Nodes = nodes
	}

	// 3. Fetch edges with their visual properties.
	edges, err := hydrateEdges(timeoutCtx, tx, id)
	if err != nil {
		return nil, err
	}
	if edges != nil {
		wf.Edges = edges
	}

	return wf, tx.Commit(timeoutCtx)
}

// UpsertWorkflow saves a workflow in a single READ COMMITTED transaction:
//  1. Upserts the workflow header (INSERT … ON CONFLICT DO UPDATE), clearing deleted_at on re-save
//  2. Deletes then re-inserts all workflow_node_instances (maps node types to node_library IDs)
//  3. Deletes then re-inserts all workflow_edges with their visual properties
//
// The delete-and-reinsert strategy keeps the write path simple at the cost of
// replacing every child row on each save.
func (r *pgStorage) UpsertWorkflow(ctx context.Context, wf *Workflow) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second) // Increased timeout for multiple operations
	defer cancel()

	tx, err := r.DB.BeginTx(timeoutCtx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted, // ReadCommitted suitable for write transactions
	})
	if err != nil {
		return fmt.Errorf("begin transaction for upsert: %w", err)
	}
	defer tx.Rollback(timeoutCtx) // Rollback on error or if not committed

	now := time.Now()
	if wf.CreatedAt.IsZero() {
		wf.CreatedAt = now
	}
	wf.ModifiedAt = now

	// 1. Upsert the main workflow entry
	_, err = tx.Exec(timeoutCtx, `
        INSERT INTO workflows (id, name, created_at, modified_at)
        VALUES ($1, $2, $3, $4)
        ON CONFLICT (id) DO UPDATE SET
            name = EXCLUDED.name,
            modified_at = EXCLUDED.modified_at,
            deleted_at = NULL;`, // Ensure workflow is 'undeleted' if upserted
		wf.ID, wf.Name, wf.CreatedAt, wf.ModifiedAt)
	if err != nil {
		return fmt.Errorf("upsert workflow header: %w", err)
	}

	// 2. Delete existing workflow_node_instances for this workflow
	_, err = tx.Exec(timeoutCtx, `
        DELETE FROM workflow_node_instances
        WHERE workflow_id = $1;`,
		wf.ID)
	if err != nil {
		return fmt.Errorf("delete old workflow node instances: %w", err)
	}

	// 3. Insert new workflow_node_instances
	// To correctly insert workflow_node_instances, we need the node_library_id for each node.
	// This requires querying the node_library table to map node_type (from wf.Nodes) to node_library.id.

	// Let's create a map to store `node_type` to `node_library_id` mappings.
	nodeLibraryIDs := make(map[string]uuid.UUID)
	nodeLibraryRows, err := tx.Query(timeoutCtx, `SELECT id, node_type FROM node_library;`)
	if err != nil {
		return fmt.Errorf("query node_library for IDs: %w", err)
	}
	defer nodeLibraryRows.Close()

	for nodeLibraryRows.Next() {
		var id uuid.UUID
		var nodeType string
		if err := nodeLibraryRows.Scan(&id, &nodeType); err != nil {
			return fmt.Errorf("scan node_library row: %w", err)
		}
		nodeLibraryIDs[nodeType] = id
	}
	if err := nodeLibraryRows.Err(); err != nil {
		return fmt.Errorf("node_library rows error: %w", err)
	}

	for _, node := range wf.Nodes {
		nodeLibraryID, ok := nodeLibraryIDs[node.Type]
		if !ok {
			return fmt.Errorf("node type %s not found in node_library", node.Type)
		}

		_, err = tx.Exec(timeoutCtx, `
            INSERT INTO workflow_node_instances (workflow_id, instance_id, node_library_id, x_pos, y_pos)
            VALUES ($1, $2, $3, $4, $5);`,
			wf.ID, node.ID, nodeLibraryID, node.Position.X, node.Position.Y)
		if err != nil {
			return fmt.Errorf("insert workflow node instance %s: %w", node.ID, err)
		}
	}

	// 4. Delete existing workflow_edges for this workflow
	_, err = tx.Exec(timeoutCtx, `
        DELETE FROM workflow_edges
        WHERE workflow_id = $1;`,
		wf.ID)
	if err != nil {
		return fmt.Errorf("delete old workflow edges: %w", err)
	}

	// 5. Insert new workflow_edges
	for _, edge := range wf.Edges {
		_, err = tx.Exec(timeoutCtx, `
            INSERT INTO workflow_edges (
                workflow_id, edge_id, source_instance_id, target_instance_id, source_handle,
                edge_type, animated, label, style_props, label_style
            ) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);`,
			wf.ID, edge.ID, edge.Source, edge.Target, edge.SourceHandle,
			edge.Type, edge.Animated, edge.Label, edge.Style, edge.LabelStyle)
		if err != nil {
			return fmt.Errorf("insert workflow edge %s: %w", edge.ID, err)
		}
	}

	return tx.Commit(timeoutCtx)
}

// DeleteWorkflow removes a workflow in a single READ COMMITTED transaction:
//  1. Hard-deletes all workflow_edges for the workflow
//  2. Hard-deletes all workflow_node_instances for the workflow
//  3. Soft-deletes the workflow header (sets deleted_at and modified_at)
//
// Returns pgx.ErrNoRows if the workflow does not exist.
func (r *pgStorage) DeleteWorkflow(ctx context.Context, id uuid.UUID) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	tx, err := r.DB.BeginTx(timeoutCtx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	if err != nil {
		return fmt.Errorf("begin transaction for delete: %w", err)
	}
	defer tx.Rollback(timeoutCtx)

	// 1. Hard delete workflow_edges for this workflow
	_, err = tx.Exec(timeoutCtx, `
        DELETE FROM workflow_edges
        WHERE workflow_id = $1;`,
		id)
	if err != nil {
		return fmt.Errorf("delete workflow edges: %w", err)
	}

	// 2. Hard delete workflow_node_instances for this workflow
	_, err = tx.Exec(timeoutCtx, `
        DELETE FROM workflow_node_instances
        WHERE workflow_id = $1;`,
		id)
	if err != nil {
		return fmt.Errorf("delete workflow node instances: %w", err)
	}

	// 3. Soft delete the main workflow entry
	result, err := tx.Exec(timeoutCtx, `
        UPDATE workflows
        SET deleted_at = $1, modified_at = $1
        WHERE id = $2;`,
		time.Now(), id)
	if err != nil {
		return fmt.Errorf("soft delete workflow header: %w", err)
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows // Indicate workflow not found
	}

	return tx.Commit(timeoutCtx)
}

// PublishWorkflow creates an immutable snapshot of the workflow's current DAG
// within a REPEATABLE READ transaction. The snapshot freezes nodes and edges
// so that future execution is decoupled from live node_library changes.
func (r *pgStorage) PublishWorkflow(ctx context.Context, id uuid.UUID) (*WorkflowSnapshot, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	tx, err := r.DB.BeginTx(timeoutCtx, pgx.TxOptions{
		IsoLevel: pgx.RepeatableRead,
	})
	if err != nil {
		return nil, fmt.Errorf("begin transaction for publish: %w", err)
	}
	defer tx.Rollback(timeoutCtx)

	// 1. Verify workflow exists and is not deleted.
	var name string
	err = tx.QueryRow(timeoutCtx, `
        SELECT name FROM workflows
        WHERE id = $1 AND deleted_at IS NULL`,
		id).Scan(&name)
	if err != nil {
		return nil, err
	}

	// 2. Hydrate current nodes and edges.
	nodes, err := hydrateNodes(timeoutCtx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("hydrate nodes for publish: %w", err)
	}

	edges, err := hydrateEdges(timeoutCtx, tx, id)
	if err != nil {
		return nil, fmt.Errorf("hydrate edges for publish: %w", err)
	}

	// 3. Marshal the DAG into JSON.
	dagData := DagData{Nodes: nodes, Edges: edges}
	if dagData.Nodes == nil {
		dagData.Nodes = []Node{}
	}
	if dagData.Edges == nil {
		dagData.Edges = []Edge{}
	}
	dagJSON, err := json.Marshal(dagData)
	if err != nil {
		return nil, fmt.Errorf("marshal dag data: %w", err)
	}

	// 4. Determine next version number.
	var nextVersion int
	err = tx.QueryRow(timeoutCtx, `
        SELECT COALESCE(MAX(version_number), 0) + 1
        FROM workflow_snapshots
        WHERE workflow_id = $1`,
		id).Scan(&nextVersion)
	if err != nil {
		return nil, fmt.Errorf("get next version: %w", err)
	}

	// 5. Insert the snapshot.
	snap := &WorkflowSnapshot{
		WorkflowID:    id,
		VersionNumber: nextVersion,
		DagData:       dagData,
	}
	err = tx.QueryRow(timeoutCtx, `
        INSERT INTO workflow_snapshots (workflow_id, version_number, dag_data)
        VALUES ($1, $2, $3)
        RETURNING id, published_at`,
		id, nextVersion, dagJSON).Scan(&snap.ID, &snap.PublishedAt)
	if err != nil {
		return nil, fmt.Errorf("insert snapshot: %w", err)
	}

	// 6. Update workflow status and active snapshot pointer.
	_, err = tx.Exec(timeoutCtx, `
        UPDATE workflows
        SET status = 'published', active_snapshot_id = $1
        WHERE id = $2`,
		snap.ID, id)
	if err != nil {
		return nil, fmt.Errorf("update workflow status: %w", err)
	}

	if err := tx.Commit(timeoutCtx); err != nil {
		return nil, fmt.Errorf("commit publish: %w", err)
	}

	return snap, nil
}

// GetActiveSnapshot retrieves the currently active snapshot for a workflow.
// Returns pgx.ErrNoRows if the workflow has no active snapshot (i.e. is a draft).
func (r *pgStorage) GetActiveSnapshot(ctx context.Context, workflowID uuid.UUID) (*WorkflowSnapshot, error) {
	timeoutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	snap := &WorkflowSnapshot{}
	var dagJSON []byte

	err := r.DB.QueryRow(timeoutCtx, `
        SELECT s.id, s.workflow_id, s.version_number, s.dag_data, s.published_at
        FROM workflow_snapshots s
        JOIN workflows w ON w.active_snapshot_id = s.id
        WHERE w.id = $1 AND w.deleted_at IS NULL`,
		workflowID).Scan(&snap.ID, &snap.WorkflowID, &snap.VersionNumber, &dagJSON, &snap.PublishedAt)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(dagJSON, &snap.DagData); err != nil {
		return nil, fmt.Errorf("unmarshal snapshot dag_data: %w", err)
	}

	return snap, nil
}
