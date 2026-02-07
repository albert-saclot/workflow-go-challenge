package storage

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Workflow represents the top-level container for a workflow graph.
// It aggregates hydrated nodes and edges after the storage layer
// joins instance data with the shared node library.
type Workflow struct {
	ID         uuid.UUID  `json:"id" db:"id"`
	Name       string     `json:"name" db:"name"`
	Nodes      []Node     `json:"nodes" db:"-"`
	Edges      []Edge     `json:"edges" db:"-"`
	CreatedAt  time.Time  `json:"createdAt" db:"created_at"`
	ModifiedAt time.Time  `json:"modifiedAt" db:"modified_at"`
	DeletedAt  *time.Time `json:"deletedAt,omitempty" db:"deleted_at"`
}

// ToFrontend returns only the fields React Flow needs: id, nodes, edges.
// This strips internal fields (name, timestamps) from the API response.
func (w *Workflow) ToFrontend() map[string]interface{} {
	return map[string]interface{}{
		"id":    w.ID,
		"nodes": w.Nodes,
		"edges": w.Edges,
	}
}

// Node is the hydrated view combining a library blueprint (type, label,
// description, metadata) with a canvas instance (position).
type Node struct {
	ID       string       `json:"id"`   // instance_id from workflow_node_instances
	Type     string       `json:"type"` // node_type from node_library
	Position NodePosition `json:"position"`
	Data     NodeData     `json:"data"`
}

type NodePosition struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// NodeData holds the display and logic properties of a node.
// Metadata is stored as raw JSON to support polymorphic node types
// (e.g., form fields, API config, condition expressions) without
// requiring separate tables per type.
type NodeData struct {
	Label       string          `json:"label"`
	Description string          `json:"description"`
	Metadata    json.RawMessage `json:"metadata"`
}

// Edge represents a directed connection between two node instances.
// SourceHandle distinguishes branches for condition nodes (e.g., "true"/"false").
type Edge struct {
	ID           string          `json:"id" db:"edge_id"`
	Source       string          `json:"source" db:"source_instance_id"`
	Target       string          `json:"target" db:"target_instance_id"`
	SourceHandle *string         `json:"sourceHandle,omitempty" db:"source_handle"`
	Type         string          `json:"type" db:"edge_type"`
	Animated     bool            `json:"animated" db:"animated"`
	Label        *string         `json:"label,omitempty" db:"label"`
	Style        json.RawMessage `json:"style,omitempty" db:"style_props"`
	LabelStyle   json.RawMessage `json:"labelStyle,omitempty" db:"label_style"`
}

// NodeLibraryEntry represents a reusable node blueprint in the shared library.
// Workflows reference these via workflow_node_instances, allowing multiple
// workflows to share the same underlying node definitions.
type NodeLibraryEntry struct {
	ID          string          `json:"id" db:"id"`
	NodeType    string          `json:"nodeType" db:"node_type"`
	Label       string          `json:"baseLabel" db:"base_label"`
	Description string          `json:"baseDescription" db:"base_description"`
	Metadata    json.RawMessage `json:"metadata" db:"metadata"`
	ModifiedAt  time.Time       `json:"modifiedAt" db:"modified_at"`
}
