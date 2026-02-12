-- V5: Workflow snapshot versioning
-- Snapshots freeze the full DAG at publish time, decoupling execution
-- from live node_library mutations. node_library is NOT modified.

ALTER TABLE workflows
    ADD COLUMN status VARCHAR(20) NOT NULL DEFAULT 'draft'
    CHECK (status IN ('draft', 'published', 'archived'));

CREATE TABLE workflow_snapshots (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id     UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
    version_number  INT NOT NULL,
    dag_data        JSONB NOT NULL,
    published_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(workflow_id, version_number)
);

CREATE INDEX idx_workflow_snapshots_workflow ON workflow_snapshots(workflow_id);

ALTER TABLE workflows
    ADD COLUMN active_snapshot_id UUID REFERENCES workflow_snapshots(id);
