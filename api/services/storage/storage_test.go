package storage

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
)

var (
	testWfID = uuid.MustParse("550e8400-e29b-41d4-a716-446655440000")
	testNow  = time.Now()
)

// setupSuccessMock configures the transaction and all three queries (header,
// nodes, edges) to return valid data matching the seeded Weather Check workflow.
func setupSuccessMock(mock pgxmock.PgxPoolIface) {
	mock.ExpectBeginTx(pgx.TxOptions{
		IsoLevel:   pgx.RepeatableRead,
		AccessMode: pgx.ReadOnly,
	})

	mock.ExpectQuery("SELECT name, created_at, modified_at").
		WithArgs(testWfID).
		WillReturnRows(
			pgxmock.NewRows([]string{"name", "created_at", "modified_at"}).
				AddRow("Weather Check System", testNow, testNow),
		)

	nodeMetadata := json.RawMessage(`{"hasHandles":{"source":true,"target":false}}`)
	mock.ExpectQuery("SELECT").
		WithArgs(testWfID).
		WillReturnRows(
			pgxmock.NewRows([]string{
				"instance_id", "node_type", "x_pos", "y_pos",
				"label", "base_description", "metadata",
			}).AddRow("start", "start", -160.0, 300.0, "Start", "Begin weather check workflow", nodeMetadata),
		)

	edgeStyle := json.RawMessage(`{"stroke":"#10b981","strokeWidth":3}`)
	edgeLabel := "Initialize"
	mock.ExpectQuery("SELECT edge_id").
		WithArgs(testWfID).
		WillReturnRows(
			pgxmock.NewRows([]string{
				"edge_id", "source_instance_id", "target_instance_id", "source_handle",
				"edge_type", "animated", "label", "style_props", "label_style",
			}).AddRow("e1", "start", "form", nil, "smoothstep", true, &edgeLabel, edgeStyle, nil),
		)

	mock.ExpectCommit()
}

func TestGetWorkflow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		setupMock func(mock pgxmock.PgxPoolIface)
		wantErr   error
		checkWf   func(t *testing.T, wf *Workflow)
	}{
		{
			name:      "success returns hydrated workflow",
			setupMock: setupSuccessMock,
			checkWf: func(t *testing.T, wf *Workflow) {
				t.Helper()

				if wf.Name != "Weather Check System" {
					t.Errorf("expected name 'Weather Check System', got %q", wf.Name)
				}

				// Verify node hydration from library join
				if len(wf.Nodes) != 1 {
					t.Fatalf("expected 1 node, got %d", len(wf.Nodes))
				}
				node := wf.Nodes[0]
				if node.ID != "start" {
					t.Errorf("expected node ID 'start', got %q", node.ID)
				}
				if node.Type != "start" {
					t.Errorf("expected node type 'start', got %q", node.Type)
				}
				if node.Position.X != -160 || node.Position.Y != 300 {
					t.Errorf("expected position (-160, 300), got (%v, %v)", node.Position.X, node.Position.Y)
				}
				if node.Data.Label != "Start" {
					t.Errorf("expected label 'Start', got %q", node.Data.Label)
				}

				// Verify edge with visual properties
				if len(wf.Edges) != 1 {
					t.Fatalf("expected 1 edge, got %d", len(wf.Edges))
				}
				edge := wf.Edges[0]
				if edge.ID != "e1" {
					t.Errorf("expected edge ID 'e1', got %q", edge.ID)
				}
				if edge.Source != "start" || edge.Target != "form" {
					t.Errorf("expected edge start->form, got %s->%s", edge.Source, edge.Target)
				}
				if !edge.Animated {
					t.Error("expected edge to be animated")
				}
				if edge.Label == nil || *edge.Label != "Initialize" {
					t.Errorf("expected edge label 'Initialize', got %v", edge.Label)
				}
			},
		},
		{
			name: "workflow not found returns ErrNoRows",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBeginTx(pgx.TxOptions{
					IsoLevel:   pgx.RepeatableRead,
					AccessMode: pgx.ReadOnly,
				})
				mock.ExpectQuery("SELECT name, created_at, modified_at").
					WithArgs(testWfID).
					WillReturnError(pgx.ErrNoRows)
				mock.ExpectRollback()
			},
			wantErr: pgx.ErrNoRows,
		},
		{
			name: "node query failure propagates error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBeginTx(pgx.TxOptions{
					IsoLevel:   pgx.RepeatableRead,
					AccessMode: pgx.ReadOnly,
				})
				// Header succeeds
				mock.ExpectQuery("SELECT name, created_at, modified_at").
					WithArgs(testWfID).
					WillReturnRows(
						pgxmock.NewRows([]string{"name", "created_at", "modified_at"}).
							AddRow("Test", testNow, testNow),
					)
				// Node query fails
				mock.ExpectQuery("SELECT").
					WithArgs(testWfID).
					WillReturnError(errors.New("connection lost"))
				mock.ExpectRollback()
			},
			wantErr: errors.New("connection lost"),
		},
		{
			name: "edge query failure propagates error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
				mock.ExpectBeginTx(pgx.TxOptions{
					IsoLevel:   pgx.RepeatableRead,
					AccessMode: pgx.ReadOnly,
				})
				// Header succeeds
				mock.ExpectQuery("SELECT name, created_at, modified_at").
					WithArgs(testWfID).
					WillReturnRows(
						pgxmock.NewRows([]string{"name", "created_at", "modified_at"}).
							AddRow("Test", testNow, testNow),
					)
				// Node query succeeds with empty results
				mock.ExpectQuery("SELECT").
					WithArgs(testWfID).
					WillReturnRows(
						pgxmock.NewRows([]string{
							"instance_id", "node_type", "x_pos", "y_pos",
							"label", "base_description", "metadata",
						}),
					)
				// Edge query fails
				mock.ExpectQuery("SELECT edge_id").
					WithArgs(testWfID).
					WillReturnError(errors.New("timeout"))
				mock.ExpectRollback()
			},
			wantErr: errors.New("timeout"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock pool: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock)

			store := &pgStorage{db: mock}
			wf, err := store.GetWorkflow(context.Background(), testWfID)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("expected error %q, got %q", tt.wantErr, err)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.checkWf != nil {
				tt.checkWf(t, wf)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unmet mock expectations: %v", err)
			}
		})
	}
}

func TestUpsertWorkflow(t *testing.T) {
	t.Parallel()
	const (
		newNodeLibraryID   = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a17"
		startNodeLibraryID = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a00"
		formNodeLibraryID  = "a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a01"
	)

	tests := []struct {
		name string
		wf   *Workflow
		setupMock func(mock pgxmock.PgxPoolIface, wf *Workflow)
		wantErr error
	}{
		{
			name: "insert new workflow successfully",
			wf: &Workflow{
				ID:   uuid.MustParse("550e8400-e29b-41d4-a716-446655440001"),
				Name: "New Test Workflow",
				Nodes: []Node{
					{
						ID:   "start-node-new",
						Type: "start",
						Position: NodePosition{X: 0, Y: 0},
					},
				},
				Edges: []Edge{
					{
						ID:     "edge-new",
						Source: "start-node-new",
						Target: "end-node-new",
					},
				},
			},
			setupMock: func(mock pgxmock.PgxPoolIface, wf *Workflow) {
				mock.ExpectBeginTx(pgx.TxOptions{
					IsoLevel: pgx.ReadCommitted,
				})

				// Expect upsert for workflow header (insert case)
				mock.ExpectExec(`INSERT INTO workflows`).
					WithArgs(wf.ID, wf.Name, pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				// Expect delete old nodes (no-op for new workflow)
				mock.ExpectExec(`DELETE FROM workflow_node_instances`).
					WithArgs(wf.ID).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))

				// Expect query for node_library_ids
				mock.ExpectQuery(`SELECT id, node_type FROM node_library`).
					WillReturnRows(pgxmock.NewRows([]string{"id", "node_type"}).
						AddRow(uuid.MustParse(startNodeLibraryID), "start").
						AddRow(uuid.MustParse(formNodeLibraryID), "form").
						AddRow(uuid.MustParse(newNodeLibraryID), "newType"))

				// Expect insert new nodes
				mock.ExpectExec(`INSERT INTO workflow_node_instances`).
					WithArgs(wf.ID, wf.Nodes[0].ID, uuid.MustParse(startNodeLibraryID), wf.Nodes[0].Position.X, wf.Nodes[0].Position.Y).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				// Expect delete old edges (no-op for new workflow)
				mock.ExpectExec(`DELETE FROM workflow_edges`).
					WithArgs(wf.ID).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))

				// Expect insert new edges
				mock.ExpectExec(`INSERT INTO workflow_edges`).
					WithArgs(wf.ID, wf.Edges[0].ID, wf.Edges[0].Source, wf.Edges[0].Target, pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				mock.ExpectCommit()
			},
			wantErr: nil,
		},
		{
			name: "update existing workflow successfully",
			wf: &Workflow{
				ID:   testWfID, // Use existing ID
				Name: "Updated Weather Check System",
				Nodes: []Node{
					{
						ID:   "start-updated",
						Type: "start",
						Position: NodePosition{X: 10, Y: 20},
					},
					{
						ID:   "form-updated",
						Type: "form",
						Position: NodePosition{X: 50, Y: 60},
					},
				},
				Edges: []Edge{
					{
						ID:     "edge-updated-1",
						Source: "start-updated",
						Target: "form-updated",
					},
					{
						ID:     "edge-updated-2",
						Source: "form-updated",
						Target: "end-updated",
					},
				},
			},
			setupMock: func(mock pgxmock.PgxPoolIface, wf *Workflow) {
				mock.ExpectBeginTx(pgx.TxOptions{
					IsoLevel: pgx.ReadCommitted,
				})

				// Expect upsert for workflow header (update case)
				mock.ExpectExec(`INSERT INTO workflows`).
					WithArgs(wf.ID, wf.Name, pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("UPDATE", 1))

				// Expect delete old nodes
				mock.ExpectExec(`DELETE FROM workflow_node_instances`).
					WithArgs(wf.ID).
					WillReturnResult(pgxmock.NewResult("DELETE", 2)) // Assuming 2 old nodes

				// Expect query for node_library_ids
				mock.ExpectQuery(`SELECT id, node_type FROM node_library`).
					WillReturnRows(pgxmock.NewRows([]string{"id", "node_type"}).
						AddRow(uuid.MustParse(startNodeLibraryID), "start").
						AddRow(uuid.MustParse(formNodeLibraryID), "form"))

				// Expect insert new nodes
				mock.ExpectExec(`INSERT INTO workflow_node_instances`).
					WithArgs(wf.ID, wf.Nodes[0].ID, uuid.MustParse(startNodeLibraryID), wf.Nodes[0].Position.X, wf.Nodes[0].Position.Y).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
				mock.ExpectExec(`INSERT INTO workflow_node_instances`).
					WithArgs(wf.ID, wf.Nodes[1].ID, uuid.MustParse(formNodeLibraryID), wf.Nodes[1].Position.X, wf.Nodes[1].Position.Y).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				// Expect delete old edges
				mock.ExpectExec(`DELETE FROM workflow_edges`).
					WithArgs(wf.ID).
					WillReturnResult(pgxmock.NewResult("DELETE", 1)) // Assuming 1 old edge

				// Expect insert new edges
				mock.ExpectExec(`INSERT INTO workflow_edges`).
					WithArgs(wf.ID, wf.Edges[0].ID, wf.Edges[0].Source, wf.Edges[0].Target, pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))
				mock.ExpectExec(`INSERT INTO workflow_edges`).
					WithArgs(wf.ID, wf.Edges[1].ID, wf.Edges[1].Source, wf.Edges[1].Target, pgxmock.AnyArg(),
						pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				mock.ExpectCommit()
			},
			wantErr: nil,
		},
		{
			name: "returns error if node type not in node_library",
			wf: &Workflow{
				ID:   uuid.MustParse("550e8400-e29b-41d4-a716-446655440002"),
				Name: "Workflow With Unknown Node",
				Nodes: []Node{
					{
						ID:   "unknown-node",
						Type: "mystery", // This type won't be in our mocked node_library
						Position: NodePosition{X: 0, Y: 0},
					},
				},
				Edges: []Edge{},
			},
			setupMock: func(mock pgxmock.PgxPoolIface, wf *Workflow) {
				mock.ExpectBeginTx(pgx.TxOptions{
					IsoLevel: pgx.ReadCommitted,
				})

				mock.ExpectExec(`INSERT INTO workflows`).
					WithArgs(wf.ID, wf.Name, pgxmock.AnyArg(), pgxmock.AnyArg()).
					WillReturnResult(pgxmock.NewResult("INSERT", 1))

				mock.ExpectExec(`DELETE FROM workflow_node_instances`).
					WithArgs(wf.ID).
					WillReturnResult(pgxmock.NewResult("DELETE", 0))

				mock.ExpectQuery(`SELECT id, node_type FROM node_library`).
					WillReturnRows(pgxmock.NewRows([]string{"id", "node_type"}).
						AddRow(uuid.MustParse(startNodeLibraryID), "start")) // "mystery" not here

				mock.ExpectRollback() // Expect rollback due to error
			},
			wantErr: errors.New("node type mystery not found in node_library"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			mock, err := pgxmock.NewPool()
			if err != nil {
				t.Fatalf("failed to create mock pool: %v", err)
			}
			defer mock.Close()

			tt.setupMock(mock, tt.wf)

			store := &pgStorage{db: mock}
			err = store.UpsertWorkflow(context.Background(), tt.wf)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				if err.Error() != tt.wantErr.Error() {
					t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if err := mock.ExpectationsWereMet(); err != nil {
				t.Errorf("unmet mock expectations: %v", err)
			}
		})
	}
}
