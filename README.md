# ⚡ Workflow Editor

A modern workflow editor app for designing and executing custom automation workflows (e.g., weather notifications). Users can visually build workflows, configure parameters, and view real-time execution results.

## 🛠️ Tech Stack

- **Frontend:** React + TypeScript, @xyflow/react (drag-and-drop), Radix UI, Tailwind CSS, Vite
- **Backend:** Go API, PostgreSQL database
- **DevOps:** Docker Compose for orchestration, hot reloading for rapid development

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

## Architecture

### Data Model: Shared Library

The persistence layer uses a three-tier structure managed via Flyway migrations:

- **`node_library`** — Global repository of reusable node definitions. Each entry holds polymorphic metadata (API configs, form fields, condition expressions) in JSONB columns.
- **`workflow_node_instances`** — Maps library nodes onto specific workflow canvases with position coordinates and label overrides.
- **`workflow_edges`** — Directed connections between node instances. Composite foreign keys `(workflow_id, instance_id)` prevent cross-workflow edges.

This separation means updating a library node (e.g. changing an API endpoint) propagates to all workflows that reference it, without touching instance-level layout data.

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

## Running Tests

```bash
cd api && go test ./... -v
```

Tests cover three packages across four test files:

| Package | File | What's tested |
| :--- | :--- | :--- |
| `services/nodes` | `node_test.go` | All node Execute() paths, factory, type conversion |
| `services/storage` | `storage_test.go` | GetWorkflow queries with pgxmock (success, not-found, scan errors) |
| `services/workflow` | `engine_test.go` | DAG validation, execution flow, branching, cancellation, partial failure |
| `services/workflow` | `workflow_test.go` | HTTP handlers (GET, POST, 404, 400, 500) |

All tests run in parallel (`t.Parallel()`) and use table-driven patterns. Storage tests use `pgxmock` to avoid requiring a running database.

## Trade-offs and Known Issues

| Decision | Benefit | Trade-off |
| :--- | :--- | :--- |
| JSONB metadata | Schema flexibility — new node types don't require DDL changes | Requires application-level validation; no DB-enforced schema on metadata |
| Shared library model | Updating a library node propagates to all workflows | Mutation side-effect risk: changing a base node alters existing workflows |
| Synchronous execution | Simple request/response model, easy to reason about | Long workflows block the HTTP request; not suitable for multi-minute executions |
| Stub clients for email/SMS | Demonstrates the interface pattern without external dependencies | No actual delivery; production would need real integrations |
| Soft deletes (`deleted_at`) | Preserves audit history for execution logs | All read queries must filter `WHERE deleted_at IS NULL` |
| Sequential node execution | Predictable ordering, straightforward variable passing | Cannot execute independent branches in parallel |

### Known Limitations

- **No execution persistence** — Execution results are returned in the HTTP response but not stored. A production system would persist runs for audit and replay.
- **No DAG validation at save time** — Cycles are caught before execution via DFS, but ideally would also be rejected when the workflow is saved (there is no save endpoint).
- **Global library mutation** — Changing a library node affects all workflows. A versioning or copy-on-write mechanism would prevent unintended side effects.
- **Single workflow query** — The storage layer only supports `GetWorkflow`. There's no list, create, update, or delete endpoint.
- **No client-level tests** — The `pkg/clients/` packages (weather, flood) make real HTTP calls with no `httptest.Server` mocks. Node tests cover the integration boundary but the clients themselves are untested in isolation.

## Future Considerations

- **Execution history** — Persist each run with its inputs, steps, and timing data. Enables audit trails and debugging.
- **Per-node retry with backoff** — Especially for 429 (rate limit) responses from shared API keys. Currently a timeout failure is terminal.
- **Async execution** — For long-running workflows, return a job ID immediately and poll or use WebSockets for results.
- **Parallel branch execution** — When independent branches exist in the graph, execute them concurrently with `errgroup`.
- **Node versioning** — Pin workflows to specific library node versions so updates don't silently change behaviour.
- **Save-time validation** — Reject invalid graphs (cycles, disconnected nodes, missing edges) at save time rather than execution time.
