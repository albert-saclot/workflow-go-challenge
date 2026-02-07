-- Seed a second workflow that uses the flood and SMS node types
-- to demonstrate that the architecture extends to new workflows
-- without code changes.

-- 1. Add a form variant for flood alerts (collects phone instead of email)
INSERT INTO node_library (id, node_type, base_label, base_description, metadata)
VALUES
    ('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a19', 'form', 'User Input', 'Collect name, phone, and city for flood alerts',
     '{"hasHandles": {"source": true, "target": true}, "inputFields": ["name", "phone", "city"], "outputVariables": ["name", "phone", "city"]}'),

    ('a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a20', 'condition', 'Check Flood Level', 'Evaluate river discharge threshold',
     '{"hasHandles": {"source": ["true", "false"], "target": true}, "conditionVariable": "discharge", "conditionExpression": "discharge {{operator}} {{threshold}}", "outputVariables": ["conditionMet"]}');

-- 2. Create the workflow
INSERT INTO workflows (id, name)
VALUES ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'Flood Alert System');

-- 3. Place nodes on the canvas
--    Layout: start → form → flood-api → condition → sms (true branch) / end (false branch)
INSERT INTO workflow_node_instances (workflow_id, instance_id, node_library_id, x_pos, y_pos)
VALUES
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'start',     'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', -160, 300),
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'form',      'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a19', 152, 300),
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'flood-api', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a18', 460, 300),
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'condition', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a20', 770, 300),
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'sms',       'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a17', 1080, 88),
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'end',       'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a16', 1340, 300);

-- 4. Wire the edges
INSERT INTO workflow_edges (workflow_id, edge_id, source_instance_id, target_instance_id, source_handle, animated, label, style_props, label_style)
VALUES
    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'e1', 'start', 'form', null,
     true, 'Initialize', '{"stroke": "#10b981", "strokeWidth": 3}', null),

    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'e2', 'form', 'flood-api', null,
     true, 'Submit Data', '{"stroke": "#3b82f6", "strokeWidth": 3}', null),

    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'e3', 'flood-api', 'condition', null,
     true, 'Flood Risk Data', '{"stroke": "#f97316", "strokeWidth": 3}', null),

    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'e4', 'condition', 'sms', 'true',
     true, E'\u2713 High Risk', '{"stroke": "#ef4444", "strokeWidth": 3}', '{"fill": "#ef4444", "fontWeight": "bold"}'),

    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'e5', 'condition', 'end', 'false',
     true, E'\u2717 Low Risk', '{"stroke": "#6b7280", "strokeWidth": 3}', '{"fill": "#6b7280", "fontWeight": "bold"}'),

    ('b7a1c3d0-5f2e-4a89-9c01-def456789abc', 'e6', 'sms', 'end', null,
     true, 'Alert Sent', '{"stroke": "#ef4444", "strokeWidth": 2}', '{"fill": "#ef4444", "fontWeight": "bold"}');
