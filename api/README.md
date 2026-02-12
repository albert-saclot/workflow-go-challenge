# Workflow API

Go backend for managing and executing workflow automations. Serves workflow definitions to the React Flow frontend and executes workflows by traversing the node graph.

## Tech Stack

- Go 1.25+
- PostgreSQL (pgx connection pool)
- Docker & Docker Compose
- Flyway (schema migrations)

## Quick Start

### Prerequisites

- Go 1.25+
- PostgreSQL
- Docker & Docker Compose (recommended)

### 1. Configure Database

Set the `DATABASE_URL` environment variable:

```
DATABASE_URL=postgres://user:password@host:port/dbname?sslmode=disable
```

### 2. Run the API

With Docker Compose (recommended):
```bash
docker-compose up --build api
```

Or run locally:
```bash
go run main.go
```

### 3. Run Tests

```bash
go test ./... -v
```

## API Endpoints

| Method | Endpoint                         | Description                        |
| ------ | -------------------------------- | ---------------------------------- |
| GET    | `/api/v1/workflows/{id}`         | Load a workflow definition         |
| POST   | `/api/v1/workflows/{id}/execute` | Execute the workflow synchronously |

### Seeded Workflows

| Workflow | UUID | Description |
| :--- | :--- | :--- |
| Weather Check System | `550e8400-e29b-41d4-a716-446655440000` | Form → Weather API → Condition → Email |
| Flood Alert System | `b7a1c3d0-5f2e-4a89-9c01-def456789abc` | Form → Flood API → Condition → SMS |
| Weather Monitor Loop | `d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef` | Form → Weather API → Condition → Email → Weather API (loop) |

### GET workflow definition

Returns the workflow in React Flow format (nodes, edges, positions).

```bash
# Weather Check System — linear: form → weather API → condition → email → end
curl http://localhost:8086/api/v1/workflows/550e8400-e29b-41d4-a716-446655440000

# Flood Alert System — linear: form → flood API → condition → SMS → end
curl http://localhost:8086/api/v1/workflows/b7a1c3d0-5f2e-4a89-9c01-def456789abc

# Weather Monitor Loop — cyclic: form → weather API → condition →(true)→ email → weather API (back-edge), condition →(false)→ end
curl http://localhost:8086/api/v1/workflows/d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef
```

Response:
```json
{
  "id": "550e8400-e29b-41d4-a716-446655440000",
  "nodes": [
    {
      "id": "start",
      "type": "start",
      "position": { "x": -160, "y": 300 },
      "data": {
        "label": "Start",
        "description": "Begin weather check workflow",
        "metadata": { "hasHandles": { "source": true, "target": false } }
      }
    }
  ],
  "edges": [
    {
      "id": "e1",
      "source": "start",
      "target": "form",
      "type": "smoothstep",
      "animated": true,
      "label": "Initialize"
    }
  ]
}
```

### POST execute workflow

Executes the workflow graph from start to end. Pass form data and condition parameters in the request body.

```bash
# Execute weather workflow — sends email if temperature > 25°C
curl -X POST http://localhost:8086/api/v1/workflows/550e8400-e29b-41d4-a716-446655440000/execute \
     -H "Content-Type: application/json" \
     -d '{
       "formData": {
         "name": "Alice",
         "email": "alice@example.com",
         "city": "Sydney"
       },
       "condition": {
         "operator": "greater_than",
         "threshold": 25
       }
     }'

# Execute flood workflow — sends SMS if flood discharge > 100 m³/s
curl -X POST http://localhost:8086/api/v1/workflows/b7a1c3d0-5f2e-4a89-9c01-def456789abc/execute \
     -H "Content-Type: application/json" \
     -d '{
       "formData": {
         "name": "Bob",
         "phone": "+61400000000",
         "city": "Brisbane"
       },
       "condition": {
         "operator": "greater_than",
         "threshold": 100
       }
     }'

# Execute loop workflow — re-checks weather and emails until temperature ≤ 25°C (or hits maxExecutionSteps)
curl -X POST http://localhost:8086/api/v1/workflows/d4e5f6a7-8b9c-0d1e-2f3a-456789abcdef/execute \
     -H "Content-Type: application/json" \
     -d '{
       "formData": {
         "name": "Alice",
         "email": "alice@example.com",
         "city": "Sydney"
       },
       "condition": {
         "operator": "greater_than",
         "threshold": 25
       }
     }'
```

Success response (all nodes passed):
```json
{
  "executedAt": "2026-02-08T10:30:00Z",
  "status": "completed",
  "steps": [
    { "nodeId": "start", "type": "start", "status": "completed" },
    { "nodeId": "form", "type": "form", "status": "completed", "output": { "name": "Alice" } },
    { "nodeId": "weather-api", "type": "integration", "status": "completed", "output": { "temperature": 28.5 } },
    { "nodeId": "condition", "type": "condition", "status": "completed", "output": { "conditionMet": true } },
    { "nodeId": "email", "type": "email", "status": "completed", "output": { "emailSent": true } },
    { "nodeId": "end", "type": "end", "status": "completed" }
  ]
}
```

Failure response (node error with partial results):
```json
{
  "executedAt": "2026-02-08T10:30:00Z",
  "status": "failed",
  "failedNode": "form",
  "error": "node \"form\" failed: missing required form field: name",
  "steps": [
    { "nodeId": "start", "type": "start", "status": "completed" },
    { "nodeId": "form", "type": "form", "status": "error", "error": "missing required form field: name" }
  ]
}
```

### Execution Safeguards

The engine validates and protects each execution:

- **Graph validation** — Before any node runs, `validateGraph` checks for structural errors: duplicate node IDs, dangling edge references, and start node protection (no incoming edges). Cycles are permitted for while-loop patterns.
- **Total timeout** — The entire execution is bounded to 60 seconds via `context.WithTimeout`.
- **Per-node timeout** — Each node gets a 10-second child context so a slow API call cannot stall the whole workflow.
- **Step limit** — Hard cap of 100 steps serves as the primary loop termination guard for cyclic workflows and catches runaway execution.
- **Request tracing** — Every request gets a `X-Request-ID` header (generated or echoed from the client) for log correlation.
- **Structured errors** — JSON responses include a machine-readable `code` field (`INVALID_ID`, `NOT_FOUND`, `INVALID_BODY`, `INTERNAL_ERROR`).
- **Read transactions** — `GetWorkflow` runs inside a `REPEATABLE READ`, read-only transaction to guarantee a consistent snapshot across the three queries (header, nodes, edges).

## Project Structure

```
api/
├── main.go                          # Entry point: wires DB, clients, deps, routes
├── go.mod
├── pkg/
│   ├── clients/                     # External service abstractions
│   │   ├── weather/client.go        # weather.Client interface + Open-Meteo impl
│   │   ├── email/client.go          # email.Client interface + stub impl
│   │   ├── sms/client.go            # sms.Client interface + stub impl
│   │   └── flood/client.go          # flood.Client interface + Open-Meteo impl
│   └── db/
│       ├── postgres.go              # Connection pool config (DefaultConfig, Connect)
│       └── migration/               # Flyway SQL migrations
│           ├── V1__create_workflow_orchestrator_system.sql  # Schema
│           ├── V2__seed_weather_workflow.sql                # Weather workflow seed
│           ├── V3__add_sms_and_flood_node_types.sql        # SMS + flood types
│           ├── V4__seed_flood_alert_workflow.sql            # Flood workflow seed
│           ├── V5__add_versioning_to_workflow_and_nodes.sql # Workflow snapshots
│           └── V6__seed_weather_monitor_loop_workflow.sql   # Loop workflow seed
└── services/
    ├── nodes/                       # Node type system
    │   ├── node.go                  # Node interface, Deps struct, New() factory
    │   ├── node_sentinel.go         # Start/End boundary nodes
    │   ├── node_form.go             # Form input validation
    │   ├── node_condition.go        # Conditional branching (configurable variable)
    │   ├── node_weather.go          # Weather API integration
    │   ├── node_email.go            # Email notification
    │   ├── node_sms.go              # SMS notification
    │   └── node_flood.go            # Flood risk API integration
    ├── storage/                     # Persistence layer
    │   ├── models.go                # Domain types (Workflow, Node, Edge, ToFrontend)
    │   ├── storage.go               # Storage interface + PostgreSQL queries
    │   └── storage_test.go          # pgxmock tests
    └── workflow/                    # HTTP service layer
        ├── service.go               # Service struct + route registration
        ├── workflow.go              # GET and POST handlers
        ├── workflow_test.go         # Handler tests (httptest)
        ├── engine.go                # Execution engine (graph validation + traversal)
        └── engine_test.go           # Engine unit tests

```

## Database

- Connection pool managed via `db.DefaultConfig()` with sensible defaults (10 max conns, 2 min, 30m lifetime)
- URI read from `DATABASE_URL` environment variable
- Schema managed via Flyway migrations in `pkg/db/migration/`
- Three-tier data model: `node_library` (blueprints) → `workflow_node_instances` (canvas placements) → `workflow_edges` (connections)

### Automated Migrations via Flyway

A dedicated `flyway` service in `docker-compose.yml` runs migrations automatically on startup — no manual SQL or migration commands required. The service:

- Uses the `flyway/flyway:10-alpine` image
- Mounts `api/pkg/db/migration/` as the SQL source directory (`/flyway/sql`)
- Waits for Postgres with `-connectRetries=60` (retries for up to 60 seconds)
- Runs `migrate` then `info` to apply pending migrations and log the final state
- Uses `-baselineOnMigrate=true -baselineVersion=0` so Flyway can adopt an existing database without failing on the initial run

The migration files follow Flyway's versioned naming convention:

| Migration | Purpose |
| :--- | :--- |
| `V1__create_workflow_orchestrator_system.sql` | Schema: tables, indexes, triggers, composite foreign keys |
| `V2__seed_weather_workflow.sql` | Seed: weather workflow with node library entries, instances, and edges |
| `V3__add_sms_and_flood_node_types.sql` | Schema + seed: SMS and flood node types in `node_library` |
| `V4__seed_flood_alert_workflow.sql` | Seed: flood alert workflow with instances and edges |
| `V5__add_versioning_to_workflow_and_nodes.sql` | Schema: workflow snapshots for versioning |
| `V6__seed_weather_monitor_loop_workflow.sql` | Seed: weather monitor loop workflow with back-edge |

Adding a new migration is: create `V7__description.sql` in `pkg/db/migration/`, restart the stack. Flyway picks it up automatically and applies it in order.

For architecture details and trade-offs, see the [root README](../README.md#architecture).
