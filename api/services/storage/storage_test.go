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

// setupSuccessMock configures all three queries (header, nodes, edges)
// to return valid data matching the seeded Weather Check workflow.
func setupSuccessMock(mock pgxmock.PgxPoolIface) {
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
}

func TestGetWorkflow(t *testing.T) {
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
				mock.ExpectQuery("SELECT name, created_at, modified_at").
					WithArgs(testWfID).
					WillReturnError(pgx.ErrNoRows)
			},
			wantErr: pgx.ErrNoRows,
		},
		{
			name: "node query failure propagates error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
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
			},
			wantErr: errors.New("connection lost"),
		},
		{
			name: "edge query failure propagates error",
			setupMock: func(mock pgxmock.PgxPoolIface) {
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
			},
			wantErr: errors.New("timeout"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
