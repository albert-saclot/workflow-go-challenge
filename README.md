# ⚡ Workflow Editor

A modern workflow editor app for designing and executing custom automation workflows (e.g., weather notifications). Users can visually build workflows, configure parameters, and view real-time execution results.

## Scope

All implementation work is in the **backend (`api/`)** and **infrastructure (`docker-compose.yml`, Flyway migrations)**. The frontend (`web/`) was not modified — the provided React Flow editor already handles rendering and interaction for the node types and edge shapes the backend serves, so no frontend changes were needed.

## 🛠️ Tech Stack

- **Frontend:** React + TypeScript, @xyflow/react (drag-and-drop), Radix UI, Tailwind CSS, Vite (provided, unchanged)
- **Backend:** Go API, PostgreSQL database
- **DevOps:** Docker Compose for orchestration, Flyway for automated migrations, hot reloading for rapid development


## 📝 Design Approach

The core problem is: build a workflow engine that's extensible (new node types without changing existing code), safe (bounded execution, no infinite loops), and honest about what it doesn't do.

**Shared Library Model** — Node definitions live in a global `node_library` table; workflows reference them via instances. I chose this over embedding definitions directly in workflows because centralised updates (e.g., changing an API endpoint) should propagate everywhere. The downside is mutation side-effects — changing a library node silently alters every workflow that uses it. A production system would need versioning or copy-on-write to prevent this, but the current schema supports the upgrade path without migration.

**Interface-Driven Extensibility** — Every external dependency (weather, email, SMS, flood) is behind an interface. This isn't just for testing — it's the primary extension mechanism. After building the initial weather workflow, I added SMS and flood nodes to prove the architecture actually extends. Each required exactly four touch points: client interface + implementation, node implementation, factory case, and a DB migration seed. Zero changes to existing code. The extensibility is demonstrated, not just claimed.

**Fail Before You Waste** — The engine runs a three-colour DFS to validate the graph is a DAG before executing any node. This is deliberate: an API call to Open-Meteo is irreversible (it has side-effects like rate limit consumption), and an email send is literally irreversible. Catching cycles upfront avoids wasting API calls and sending partial notifications on a graph that can never terminate. The step limit (100) is a backstop, not the primary safeguard.

**Business Errors Are Not Server Errors** — Node failures return HTTP 200 with `status: "failed"` and partial results, not 500. A weather API returning bad data is a business outcome — the engine ran correctly, a node within the workflow produced an error. This lets clients inspect completed steps for debugging and avoids conflating "the server crashed" with "the user submitted an invalid city name." That said, 422 would also be defensible for input validation failures.

## 🏛️ Architecture

### Data Model: Shared Library

The persistence layer uses a three-tier structure managed via Flyway migrations:

- **`node_library`** — Global repository of reusable node definitions. Each entry holds polymorphic metadata (API configs, form fields, condition expressions) in JSONB columns.
- **`workflow_node_instances`** — Maps library nodes onto specific workflow canvases with position coordinates and label overrides.
- **`workflow_edges`** — Directed connections between node instances. Composite foreign keys `(workflow_id, instance_id)` prevent cross-workflow edges.

This separation means updating a library node (e.g. changing an API endpoint) propagates to all workflows that reference it, without touching instance-level layout data.

#### ER Diagram

```
┌──────────────────────┐             ┌───────────────────┐
│     node_library     │             │     workflows     │
│──────────────────────│             │───────────────────│
│ id          (PK,UUID)│◄─────┐      │ id     (PK, UUID) │
│ node_type   VARCHAR  │      │      │ name     VARCHAR  │
│ base_label  VARCHAR  │      │      │ deleted_at  TSTZ  │
│ base_description TEXT│      │      └─────────┬─────────┘
│ metadata      JSONB  │      │                │
│ deleted_at     TSTZ  │      │                │ 1
└──────────────────────┘      │                │
                              │                ▼ *
┌─────────────────────────────┴────────────────────────────┐
│              workflow_node_instances                     │
│──────────────────────────────────────────────────────────│
│ workflow_id    (PK, FK → workflows.id) ON DELETE CASCADE │
│ instance_id    (PK, VARCHAR)  'start', 'weather-api', …  │
│ node_library_id (FK → node_library.id)                   │
│ x_pos, y_pos    FLOAT                                    │
└──────────┬──────────────────────────────┬────────────────┘
           │                              │
           │ source_instance_id           │ target_instance_id
           │ (composite FK)               │ (composite FK)
           ▼                              ▼
┌────────────────────────────────────────────────────────────────┐
│                    workflow_edges                              │
│────────────────────────────────────────────────────────────────│
│ workflow_id          (PK, FK → workflows.id) ON DELETE CASCADE │
│ edge_id              (PK, VARCHAR)                             │
│ source_instance_id   (FK → (workflow_id, instance_id))         │
│ target_instance_id   (FK → (workflow_id, instance_id))         │
│ source_handle         VARCHAR    (condition branch routing)    │
│ edge_type, animated, label, style_props, label_style           │
└────────────────────────────────────────────────────────────────┘
```

Key constraints:
- **Composite PK** on `workflow_node_instances(workflow_id, instance_id)` — instance IDs are human-readable strings, unique per workflow
- **Composite FKs** on `workflow_edges` — both `source_instance_id` and `target_instance_id` reference `(workflow_id, instance_id)`, preventing cross-workflow edges at the DB level
- **Soft deletes** on `workflows` and `node_library` (`deleted_at` column); child rows use `ON DELETE CASCADE` for hard deletes
- Audit columns (`created_at`, `modified_at`) on all tables with auto-update triggers (omitted from diagram for clarity)

### Node Type System

Each node type implements a common `Node` interface with two responsibilities:

- **`ToJSON()`** — Serializes back to the React Flow shape the frontend expects. Passes raw JSONB metadata through without transformation, so the frontend always gets exactly what the database stores.
- **`Execute(ctx, nodeContext)`** — Runs the node's logic during workflow execution. Returns output variables that flow into downstream nodes, and an optional branch identifier for condition routing.

Current node types:

| Type | Purpose | External Dependency |
| :--- | :--- | :--- |
| `start` / `end` | Sentinel boundaries marking graph entry and exit | None |
| `form` | Validates required input fields from user-submitted data | None |
| `condition` | Evaluates an expression and sets a branch (`"true"`/`"false"`) for edge routing | None |
| `weather` | Fetches current temperature from Open-Meteo API for a given city | `weather.Client` |
| `email` | Sends an email notification with template variable substitution | `email.Client` |
| `sms` | Sends an SMS notification with template variable substitution | `sms.Client` |
| `flood` | Fetches flood risk level from Open-Meteo flood API for a given city | `flood.Client` |

The `sms` and `flood` node types were added specifically to validate that the architecture extends cleanly. Each required only four touch points — no changes to existing code:

1. A client interface and implementation in `pkg/clients/{sms,flood}/`
2. A node implementation in `services/nodes/node_{sms,flood}.go`
3. A new case in the `New()` factory function in `services/nodes/node.go`
4. A `V3` migration that `INSERT`s the new types into `node_library`

### Execution Engine

The `executeWorkflow` function walks the workflow DAG from the start node:

1. Constructs typed node instances from stored metadata via the factory
2. Builds an adjacency list from edges
3. **Validates the graph is a DAG** before executing any nodes (see below)
4. Executes nodes sequentially, merging each node's output variables into a shared context
5. Follows outgoing edges — for condition nodes, matches the branch result (`"true"`/`"false"`) against edge `sourceHandle` values

#### Pre-execution DAG Validation

Before any node runs, the engine validates the workflow graph using depth-first search with three-colour marking. Each node starts as **white** (unvisited). When the DFS enters a node it marks it **grey** (visiting). When all of a node's children are fully explored, it becomes **black** (done).

If the DFS reaches a node that is already grey, that means we followed an edge back to a node we're still exploring — a cycle. The engine rejects the workflow immediately with an error, before making any API calls or side effects.

```
start ──▶ A ──▶ B ──▶ end     ✓ valid DAG (all nodes go white → grey → black)

start ──▶ A ──▶ B
          ▲     │
          └─────┘              ✗ cycle detected (B points back to grey A)
```

This catches malformed workflows upfront rather than wasting API calls on a graph that can never terminate.

#### Safeguards

- **DAG validation** — Three-colour DFS rejects cycles before execution begins
- **Total workflow timeout** — 60-second cap on the entire execution, enforced via `context.WithTimeout`
- **Per-node timeout** — Each node runs under its own 10-second timeout to prevent slow API calls from blocking the workflow
- **Context cancellation** — Checks `ctx.Err()` each iteration so client disconnects stop execution
- **Max step limit** — Hard cap of 100 steps guards against edge cases the DFS might miss
- **Partial failure results** — When a node fails, the response includes all completed steps plus the failed node with error details, returned as HTTP 200 with `status: "failed"` (not 500, since the engine itself didn't crash)
- **Request ID tracing** — Each request gets a unique ID (or reuses the client's `X-Request-ID`) for log correlation
- **Structured error codes** — JSON error responses include machine-readable codes (`INVALID_ID`, `NOT_FOUND`, `INTERNAL_ERROR`) so clients can distinguish between retryable and non-retryable failures

### External Client Abstraction

External API calls (weather, email, SMS, flood) are behind interfaces in `pkg/clients/`. Node implementations depend on the interface, not the concrete client. The `Deps` struct carries all client instances through dependency injection, making nodes unit-testable without network calls.

### API Endpoints

| Method | Path | Handler | Description |
| :--- | :--- | :--- | :--- |
| `GET` | `/workflows/{id}` | `HandleGetWorkflow` | Load workflow definition for React Flow |
| `POST` | `/workflows/{id}/execute` | `HandleExecuteWorkflow` | Execute workflow with input variables |
| `PUT` | `/workflows/{id}` | `HandleSaveWorkflow` | Create or update workflow (storage-ready) |
| `DELETE` | `/workflows/{id}` | `HandleDeleteWorkflow` | Soft-delete workflow (storage-ready) |

`PUT` and `DELETE` are backed by fully implemented and tested storage methods (`UpsertWorkflow`, `DeleteWorkflow`) — only the thin HTTP handlers remain to be wired.

#### Request Flow

**`GET /workflows/{id}`** — Load a workflow definition for the frontend editor.

```
Client                    Handler                   Storage (REPEATABLE READ tx)
  │                          │                              │
  │  GET /workflows/{id}     │                              │
  │─────────────────────────►│  parse UUID                  │
  │                          │─────────────────────────────►│  SELECT workflow header
  │                          │                              │  (WHERE deleted_at IS NULL)
  │                          │                              │  SELECT instances JOIN node_library
  │                          │                              │  SELECT edges
  │                          │◄─────────────────────────────│  Workflow{Nodes, Edges}
  │                          │                              │
  │                          │  for each node:              │
  │                          │    factory → typed Node      │
  │                          │    node.ToJSON()             │
  │                          │                              │
  │  200 {id, nodes, edges}  │                              │
  │◄─────────────────────────│                              │
```

Three queries in a single `REPEATABLE READ` read-only transaction ensure a consistent snapshot — no partial reads if a concurrent save is in progress.

**`POST /workflows/{id}/execute`** — Run the workflow graph end-to-end.

```
Client                    Handler                   Engine
  │                          │                         │
  │  POST /execute           │                         │
  │  {formData, condition}   │                         │
  │─────────────────────────►│  parse UUID + body      │
  │                          │  flatten inputs         │
  │                          │  GetWorkflow(...)       │
  │                          │────────────────────────►│  build typed nodes (factory)
  │                          │                         │  build adjacency list
  │                          │                         │  validateDAG (3-colour DFS)
  │                          │                         │  ──── execution loop ────
  │                          │                         │  for each node (BFS):
  │                          │                         │    ctx with 10s timeout
  │                          │                         │    node.Execute(ctx, vars)
  │                          │                         │    merge outputs → vars
  │                          │                         │    follow edges (branch routing)
  │                          │                         │  ────────────────────────
  │                          │◄────────────────────────│  ExecutionResult{status, steps}
  │  200 {status, steps,     │                         │
  │       executedAt}        │                         │
  │◄─────────────────────────│                         │
```

Business failures (node errors, bad input) return 200 with `status: "failed"` and partial results. Only infrastructure errors (corrupt metadata, marshal failures) return 5xx.

**`PUT /workflows/{id}`** — Save or update a workflow definition.

```
Client                    Handler                   Storage (READ COMMITTED tx)
  │                          │                              │
  │  PUT /workflows/{id}     │                              │
  │  {name, nodes, edges}    │                              │
  │─────────────────────────►│  parse UUID + body           │
  │                          │─────────────────────────────►│  INSERT workflow … ON CONFLICT
  │                          │                              │    DO UPDATE (clears deleted_at)
  │                          │                              │  DELETE old node instances
  │                          │                              │  SELECT node_library (type → ID map)
  │                          │                              │  INSERT new node instances
  │                          │                              │  DELETE old edges
  │                          │                              │  INSERT new edges
  │                          │                              │  COMMIT
  │  200 OK                  │◄─────────────────────────────│
  │◄─────────────────────────│                              │
```

Delete-and-reinsert for child rows keeps the write path simple. The `ON CONFLICT` upsert means saving a previously deleted workflow un-deletes it.

**`DELETE /workflows/{id}`** — Soft-delete a workflow.

```
Client                    Handler                   Storage (READ COMMITTED tx)
  │                          │                              │
  │  DELETE /workflows/{id}  │                              │
  │─────────────────────────►│  parse UUID                  │
  │                          │─────────────────────────────►│  DELETE edges (hard)
  │                          │                              │  DELETE node instances (hard)
  │                          │                              │  UPDATE workflows SET deleted_at
  │                          │                              │  (0 rows → 404)
  │                          │                              │  COMMIT
  │  204 No Content          │◄─────────────────────────────│
  │◄─────────────────────────│                              │
```

Child rows are hard-deleted (they have no independent audit value); the workflow header is soft-deleted to preserve the audit trail. The header remains queryable for historical reference but is excluded from active queries by the `WHERE deleted_at IS NULL` filter.

### Project Structure

```
api/
├── main.go                          # Wiring: DB, clients, deps, routes
├── pkg/
│   ├── clients/
│   │   ├── weather/client.go        # Open-Meteo weather API
│   │   ├── email/client.go          # Email (stub)
│   │   ├── sms/client.go            # SMS (stub)
│   │   └── flood/client.go          # Open-Meteo flood API
│   └── db/
│       ├── postgres.go              # Connection pool config
│       └── migration/               # Flyway SQL migrations (V1-V4)
└── services/
    ├── nodes/
    │   ├── node.go                  # Interface, Deps, factory
    │   ├── node_sentinel.go         # Start/End boundaries
    │   ├── node_form.go             # Form input validation
    │   ├── node_condition.go        # Conditional branching
    │   ├── node_weather.go          # Weather API integration
    │   ├── node_email.go            # Email notification
    │   ├── node_sms.go              # SMS notification
    │   └── node_flood.go            # Flood risk API
    ├── storage/
    │   ├── models.go                # Domain types, ToFrontend()
    │   ├── storage.go               # DB queries (3-way join)
    │   └── storage_test.go          # pgxmock tests
    └── workflow/
        ├── service.go               # Service + route registration
        ├── workflow.go              # HTTP handlers
        ├── workflow_test.go         # Handler tests
        ├── engine.go                # DAG execution engine
        └── engine_test.go           # Engine unit tests
```

## Trade-offs and Decisions

| Decision | Benefit | Trade-off |
| :--- | :--- | :--- |
| JSONB metadata | Schema flexibility — new node types don't require DDL changes | Requires application-level validation; no DB-enforced schema on metadata |
| Shared library model | Updating a library node propagates to all workflows | Mutation side-effect risk: changing a base node alters existing workflows |
| Synchronous execution | Simple request/response model, easy to reason about | Long workflows block the HTTP request; not suitable for multi-minute executions |
| Stub clients for email/SMS | Demonstrates the interface pattern without external dependencies | No actual delivery; production would need real integrations |
| Soft deletes (`deleted_at`) | Preserves audit history for execution logs | All read queries must filter `WHERE deleted_at IS NULL` |
| Sequential node execution | Predictable ordering, straightforward variable passing | Cannot execute independent branches in parallel |
| Flat variable namespace | Simple — nodes read/write to `map[string]any` | Two nodes writing the same key overwrite each other silently |
| `REPEATABLE READ` for reads | Consistent snapshot across the 3-query GetWorkflow join | Slightly higher isolation overhead than `READ COMMITTED` |
| Composite foreign keys on edges | DB-level prevention of cross-workflow edges | More complex schema; requires composite primary keys on instances |

**On synchronous execution**: I chose this deliberately over async (job queue + polling) because the workflows are short-lived (a few API calls) and the request/response model is simpler to reason about and debug. The 60-second total timeout is the natural ceiling. If workflows needed minutes, I'd return a job ID and use WebSockets or polling — but that complexity isn't justified for the current use case.

**On stub clients**: The interfaces are the deliverable, not the implementations. Swapping `sms.NewStubClient()` for a Twilio implementation requires implementing a 1-method interface. The node code doesn't change. I chose stubs over real integrations to keep the submission self-contained and runnable without API keys.

**On the flat variable namespace**: All node outputs merge into a single `map[string]any`. This works for linear workflows where each variable has one producer. For complex DAGs with parallel branches, I'd namespace outputs (e.g., `nodeId.variableName`) or use an immutable context with copy-on-write semantics. The current design is intentionally simple for the workflow shapes this system supports.

**On the "integration" type name**: The weather node maps to `"integration"` in the factory and DB, while SMS and flood use their own named types (`"sms"`, `"flood"`). This is a leftover from the original provided schema — the frontend renders node appearance based on this type string, so renaming it would break the contract. The file is named `node_weather.go` to signal the intent. In a real system, I'd coordinate a rename with a frontend update.

### Known Limitations

- **No execution persistence** — Execution results are returned in the HTTP response but not stored. A production system would persist runs for audit and replay.
- **No DAG validation at save time** — Cycles are caught before execution via DFS, but ideally would also be rejected when the workflow is saved.
- **Global library mutation** — Changing a library node affects all workflows. A versioning or copy-on-write mechanism would prevent unintended side effects.
- **Save and delete are persistence-layer only (HTTP handlers out of scope)** — `UpsertWorkflow` and `DeleteWorkflow` are fully implemented and tested at the storage layer, but no HTTP endpoints expose them yet. The handlers were out of scope for this submission; the storage layer was prioritised because the three-tier data model makes persistence the complex part of these operations.

  - **`UpsertWorkflow`** — Single `READ COMMITTED` transaction that upserts the workflow header (`INSERT … ON CONFLICT DO UPDATE`, clearing `deleted_at` on re-save), then deletes and re-inserts all node instances (mapping node types to `node_library` IDs) and edges.
  - **`DeleteWorkflow`** — Single `READ COMMITTED` transaction that hard-deletes all edges and node instances, then soft-deletes the workflow header (`deleted_at` + `modified_at`). Returns `pgx.ErrNoRows` if the workflow does not exist.

  Both follow the same transactional pattern as `GetWorkflow` and would need only thin HTTP handlers to be exposed as `PUT /{id}` and `DELETE /{id}`.
- **No client-level tests** — The `pkg/clients/` packages (weather, flood) make real HTTP calls with no `httptest.Server` mocks. Node tests cover the integration boundary but the clients themselves are untested in isolation.

## What I'd Build Next

Ordered by impact, not by ease of implementation:

1. **Execution persistence** — Persist each run with its inputs, steps, and timing data. The `ExecutionResponse` struct is already the right shape for an `execution_runs` table. This is the highest-impact gap because without it there's no audit trail, no replay, and no way to debug failed workflows after the HTTP response is gone.

2. **Observability** — OpenTelemetry spans per node execution (the engine loop is the natural instrumentation point), Prometheus counters for execution counts and latencies, and structured log correlation via the existing request ID middleware. `slog` is already in place; this is plumbing, not architecture.

3. **Save-time DAG validation** — Currently cycles are caught at execution time. Validating at save time (the `UpsertWorkflow` path) would prevent users from saving broken workflows. The `validateDAG` function already exists and is decoupled from execution — it just needs to be called from the save handler.

4. **Client-level tests** — The `pkg/clients/` HTTP clients have zero test coverage. `httptest.Server` mocks would catch timeout handling, error parsing, and malformed response edge cases that the node-level mocks don't exercise.

5. **Node versioning** — Pin workflows to specific library node versions via content-addressable metadata hashing or explicit version columns. This directly addresses the shared library mutation risk. The schema change is straightforward (add a `version` column to `node_library`, reference it from instances), but the migration path for existing data needs care.

6. **Parallel branch execution** — When independent branches exist in the graph (no data dependency), execute them concurrently with `errgroup`. The engine's adjacency list already supports multiple outgoing edges per node; the change is in the execution loop, not the data model.

## 🚀 Quick Start

### Prerequisites

- Docker & Docker Compose (recommended for development)
- Node.js v18+ (for local frontend development)
- Go v1.25+ (for local backend development)

> **Tip:** Node.js and Go are only required if you want to run frontend or backend outside Docker.

### 1. Start All Services

```bash
docker-compose up --build
```

- This launches frontend, backend, and database with hot reloading enabled for code changes.
- To stop and clean up:
  ```bash
  docker-compose down
  ```

### 2. Access Applications

- **Frontend (Workflow Editor):** [http://localhost:3003](http://localhost:3003)
- **Backend API:** [http://localhost:8086](http://localhost:8086)
- **Database:** PostgreSQL on `localhost:5876`

### 3. Verify Setup

1. Open [http://localhost:3003](http://localhost:3003) in your browser.
2. You should see the workflow editor with sample nodes.

## 🔧 Development Workflow

### 🌐 Frontend

- Edit files in `web/src/` and see changes instantly at [http://localhost:3003](http://localhost:3003) (hot reloading via Vite).

### 🖥️ Backend

- Edit files in `api/` and changes are reflected automatically (hot reloading in Docker).
- If you add new dependencies or make significant changes, rebuild the API container:
  ```bash
  docker-compose up --build api
  ```

### 🗄️ Database

- Schema/configuration details: see [API README](api/README.md#database)
- After schema changes or migrations, restart the database:
  ```bash
  docker-compose restart postgres
  ```
- To apply schema changes to the API after updating the database:
  ```bash
  docker-compose restart api
  ```

## Testing Strategy

```bash
cd api && go test ./... -v
```

Tests cover three packages across 11 test files:

| Package | Files | What's tested |
| :--- | :--- | :--- |
| `services/nodes` | `node_test.go` + 7 per-type test files | Factory, ToJSON (all 8 node types), Execute paths for every node type |
| `services/storage` | `storage_test.go` | GetWorkflow, UpsertWorkflow, DeleteWorkflow queries with pgxmock |
| `services/workflow` | `engine_test.go` | DAG validation, execution flow, branching, cancellation, partial failure |
| `services/workflow` | `workflow_test.go` | HTTP handlers (GET, POST, 404, 400, 500) |

**Why table-driven**: Every test function uses the `[]struct{ name; input; want }` pattern with `t.Run` subtests. This makes it trivial to add new cases (e.g., adding a "custom variable" condition test is one struct literal, not a new function) and produces clear output showing exactly which case failed.

**Why parallel**: All tests call `t.Parallel()` at both the top-level and subtest level. This catches accidental shared state — if two subtests mutate the same variable, the race detector flags it immediately.

**Why pgxmock over testcontainers**: Storage tests mock the database via `pgxmock.PgxPoolIface`. This keeps tests fast and dependency-free (no running Postgres required), at the cost of not catching real query issues. The trade-off is intentional — integration tests against a real database would be the next layer to add, but unit tests with exact query matching catch the most common bugs (wrong column order, missing WHERE clauses).

**Why mock interfaces, not mock frameworks**: Node tests use hand-written mocks (e.g., `mockWeatherClient`) rather than generated mocks. For interfaces with 1-2 methods, a 5-line struct is simpler and more readable than pulling in `gomock` or `mockery`. The mock is right there in the test file.
