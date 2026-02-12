-- Seed a while-loop workflow that re-checks weather and sends alerts
-- until the temperature drops below threshold.
--
-- Graph:
--   start -> form -> weather-api -> condition --(true)--> email -> weather-api (back-edge)
--                                             --(false)--> end
--
-- The condition's true branch loops: weather-api -> condition -> email -> weather-api.
-- Terminates when condition evaluates false, or at maxExecutionSteps (100).
-- No new node_library entries needed — reuses the Weather Check System nodes.

-- 1. Create the workflow
INSERT INTO workflows (id, name)
VALUES ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'Weather Monitor Loop');

-- 2. Place nodes on the canvas
--    The loop body (weather-api -> condition -> email) sits in the middle,
--    with the back-edge from email returning to weather-api.
INSERT INTO workflow_node_instances (workflow_id, instance_id, node_library_id, x_pos, y_pos)
VALUES
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'start',       'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a11', -160, 300),
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'form',        'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a12',  150, 300),
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'weather-api', 'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a13',  460, 300),
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'condition',   'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a14',  770, 300),
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'email',       'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a15', 1080,  88),
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'end',         'a0eebc99-9c0b-4ef8-bb6d-6bb9bd380a16', 1080, 512);

-- 3. Wire the edges (e6 is the back-edge that creates the loop)
INSERT INTO workflow_edges (workflow_id, edge_id, source_instance_id, target_instance_id, source_handle, animated, label, style_props, label_style)
VALUES
    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'e1', 'start', 'form', null,
     true, 'Initialize', '{"stroke": "#10b981", "strokeWidth": 3}', null),

    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'e2', 'form', 'weather-api', null,
     true, 'Submit Data', '{"stroke": "#3b82f6", "strokeWidth": 3}', null),

    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'e3', 'weather-api', 'condition', null,
     true, 'Temperature Data', '{"stroke": "#f97316", "strokeWidth": 3}', null),

    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'e4', 'condition', 'email', 'true',
     true, E'\u2713 Too Hot', '{"stroke": "#ef4444", "strokeWidth": 3}', '{"fill": "#ef4444", "fontWeight": "bold"}'),

    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'e5', 'condition', 'end', 'false',
     true, E'\u2717 All Clear', '{"stroke": "#6b7280", "strokeWidth": 3}', '{"fill": "#6b7280", "fontWeight": "bold"}'),

    ('d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef', 'e6', 'email', 'weather-api', null,
     true, E'\u21bb Re-check', '{"stroke": "#a855f7", "strokeWidth": 2, "strokeDasharray": "5,5"}', '{"fill": "#a855f7", "fontWeight": "bold"}');
