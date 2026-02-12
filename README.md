# ⚡ Workflow Editor

A modern workflow editor app for designing and executing custom automation workflows (e.g., weather notifications). Users can visually build workflows, configure parameters, and view real-time execution results.

## Scope

All implementation work is in the **backend (`api/`)** and **infrastructure (`docker-compose.yml`, Flyway migrations)**. The frontend (`web/`) was not modified — the provided React Flow editor already handles rendering and interaction for the node types and edge shapes the backend serves, so no frontend changes were needed.

## 🛠️ Tech Stack

- **Frontend:** React + TypeScript, @xyflow/react (drag-and-drop), Radix UI, Tailwind CSS, Vite (provided, unchanged)
- **Backend:** Go API, PostgreSQL database
- **DevOps:** Docker Compose for orchestration, Flyway for automated migrations, hot reloading for rapid development


## 📝 Design Approach

The core problem is: build a workflow engine that's extensible (new node types without changing existing code), safe (bounded execution, controlled looping), and honest about what it doesn't do.

**Shared Library Model** — Node definitions live in a global `node_library` table; workflows reference them via instances. I chose this over embedding definitions directly in workflows because centralised updates (e.g., changing an API endpoint) should propagate everywhere. The downside is mutation side-effects — changing a library node silently alters every workflow that uses it. A production system would need versioning or copy-on-write to prevent this, but the current schema supports the upgrade path without migration.

**Interface-Driven Extensibility** — Every external dependency (weather, email, SMS, flood) is behind an interface. This isn't just for testing — it's the primary extension mechanism. After building the initial weather workflow, I added SMS and flood nodes to prove the architecture actually extends. Each required exactly four touch points: client interface + implementation, node implementation, factory case, and a DB migration seed. Zero changes to existing code. The extensibility is demonstrated, not just claimed.

**Fail Before You Waste** — Before executing any node, the engine validates the graph structure: duplicate node IDs, dangling edge references, and start node protection. These catch real authoring errors that would cause confusing runtime failures. Cycles are intentionally allowed — they enable while-loop patterns where a condition node controls re-entry. The `maxExecutionSteps` limit (100) serves as the loop termination guard, bounding execution whether the loop exits cleanly via a condition or runs to exhaustion.

**Business Errors Are Not Server Errors** — Node failures return HTTP 200 with `status: "failed"` and partial results, not 500. A weather API returning bad data is a business outcome — the engine ran correctly, a node within the workflow produced an error. This lets clients inspect completed steps for debugging and avoids conflating "the server crashed" with "the user submitted an invalid city name." That said, 422 would also be defensible for input validation failures.

## 🏛️ Architecture

### Data Model: Shared Library

The persistence layer uses a three-tier structure managed via Flyway migrations:

- **`node_library`** — Global repository of reusable node definitions. Each entry holds polymorphic metadata (API configs, form fields, condition expressions) in JSONB columns.
- **`workflow_node_instances`** — Maps library nodes onto specific workflow canvases with position coordinates and label overrides.
- **`workflow_edges`** — Directed connections between node instances. Composite foreign keys `(workflow_id, instance_id)` prevent cross-workflow edges.

This separation means updating a library node (e.g. changing an API endpoint) propagates to all workflows that reference it, without touching instance-level layout data.

#### ER Diagram

Current tables plus the proposed `workflow_triggers` table (see [Workflow Triggers](#workflow-triggers) below).

```
┌──────────────────┐         ┌──────────────────────────┐
│   node_library   │         │        workflows         │
│──────────────────│         │──────────────────────────│
│ id (PK)          │◄────┐   │ id (PK)                  │
│ node_type        │     │   │ name                     │
│ base_label       │     │   │ status                   │
│ metadata (JSONB) │     │   │ active_snapshot_id (FK)──│──┐
└──────────────────┘     │   └──────────┬───────────────┘  │
                         │       │      │      │           │
                         │       │      │      │           │
           ┌─────────────┘       │      │      │           │
           │                     │      │      │           │
┌──────────┴───────────────┐     │      │      │     ┌─────┴────────────────┐
│ workflow_node_instances  │     │      │      │     │ workflow_snapshots   │
│──────────────────────────│     │      │      │     │──────────────────────│
│ workflow_id (PK,FK)      │◄────┘      │      │     │ id (PK)              │
│ instance_id (PK)         │◄──┐        │      │     │ workflow_id (FK)     │
│ node_library_id (FK)     │   │        │      └────►│ version_number       │
│ x_pos, y_pos             │   │        │            │ dag_data (JSONB)     │
└──────────────────────────┘   │        │            └──────────────────────┘
                               │        │
┌──────────────────────────┐   │        │      ┌──────────────────────────┐
│    workflow_edges        │   │        │      │  workflow_triggers       │
│──────────────────────────│   │        │      │  (proposed)              │
│ workflow_id (PK,FK)      │───┘        │      │──────────────────────────│
│ edge_id (PK)             │            └─────►│ id (PK)                  │
│ source_instance_id (FK)  │                   │ workflow_id (FK)         │
│ target_instance_id (FK)  │                   │ trigger_type             │
│ source_handle            │                   │ cron_expression          │
│ label, style (JSONB)     │                   │ webhook_token (UNIQUE)   │
└──────────────────────────┘                   │ default_inputs (JSONB)   │
                                               │ enabled, next_trigger    │
                                               └──────────────────────────┘
```

Key relationships:
- **Composite PK** on `workflow_node_instances(workflow_id, instance_id)` — instance IDs are human-readable strings, unique per workflow
- **Composite FKs** on `workflow_edges` — both `source_instance_id` and `target_instance_id` reference `(workflow_id, instance_id)`, preventing cross-workflow edges at the DB level
- **Soft deletes** on `workflows` and `node_library` (`deleted_at` column); child rows use `ON DELETE CASCADE` for hard deletes
- `workflow_snapshots` freezes the full graph as JSONB; `workflows.active_snapshot_id` points to the current published version
- `workflow_triggers` (proposed) associates one or more triggers with a workflow — schedule config, webhook tokens, and operational state
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

The `executeWorkflow` function walks the workflow graph from the start node:

1. Constructs typed node instances from stored metadata via the factory
2. Builds an adjacency list from edges
3. **Validates graph structure** before executing any nodes (see below)
4. Executes nodes sequentially, merging each node's output variables into a shared context
5. Follows outgoing edges — for condition nodes, matches the branch result (`"true"`/`"false"`) against edge `sourceHandle` values

#### Pre-execution Graph Validation

Before any node runs, the engine calls `validateGraph` to catch structural authoring errors:

- **Duplicate node IDs** — Two nodes with the same ID would cause ambiguous routing
- **Dangling edge references** — Every edge source and target must reference an existing node
- **Start node protection** — The start node must have no incoming edges (prevents loops that swallow the entry point)

Cycles are **permitted** — they enable while-loop patterns where a condition node controls re-entry into the loop body. Runaway execution is bounded by `maxExecutionSteps` (100).

```
start ──▶ form ──▶ weather ──▶ condition ──▶ end
                       ▲           │
                       │          true
                       │           │
                       └── email ◀─┘          ✓ valid loop (condition controls re-entry)

start ──▶ A ──▶ start                        ✗ rejected (start has incoming edge)
```

This catches malformed workflows upfront while still supporting intentional looping patterns.

#### Safeguards

- **Graph validation** — Structural checks (duplicate IDs, dangling edges, start node protection) reject malformed workflows before execution begins
- **Total workflow timeout** — 60-second cap on the entire execution, enforced via `context.WithTimeout`
- **Per-node timeout** — Each node runs under its own 10-second timeout to prevent slow API calls from blocking the workflow
- **Context cancellation** — Checks `ctx.Err()` each iteration so client disconnects stop execution
- **Max step limit** — Hard cap of 100 steps serves as the primary loop termination guard and catches runaway execution in cyclic workflows
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
│       └── migration/               # Flyway SQL migrations (V1-V6)
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
        ├── engine.go                # Execution engine (graph validation + traversal)
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
| Cycles allowed for loops | Enables while-loop patterns with condition nodes | Unconditional cycles run to `maxExecutionSteps`; no variable mutation means loops can't self-terminate yet |

**On synchronous execution**: I chose this deliberately over async (job queue + polling) because the workflows are short-lived (a few API calls) and the request/response model is simpler to reason about and debug. The 60-second total timeout is the natural ceiling. If workflows needed minutes, I'd return a job ID and use WebSockets or polling — but that complexity isn't justified for the current use case.

**On stub clients**: The interfaces are the deliverable, not the implementations. Swapping `sms.NewStubClient()` for a Twilio implementation requires implementing a 1-method interface. The node code doesn't change. I chose stubs over real integrations to keep the submission self-contained and runnable without API keys.

**On the flat variable namespace**: All node outputs merge into a single `map[string]any`. This works for linear workflows where each variable has one producer. For complex DAGs with parallel branches, I'd namespace outputs (e.g., `nodeId.variableName`) or use an immutable context with copy-on-write semantics. The current design is intentionally simple for the workflow shapes this system supports.

**On the "integration" type name**: The weather node maps to `"integration"` in the factory and DB, while SMS and flood use their own named types (`"sms"`, `"flood"`). This is a leftover from the original provided schema — the frontend renders node appearance based on this type string, so renaming it would break the contract. The file is named `node_weather.go` to signal the intent. In a real system, I'd coordinate a rename with a frontend update.

### Known Limitations

- **No execution persistence** — Execution results are returned in the HTTP response but not stored. A production system would persist runs for audit and replay.
- **No graph validation at save time** — Structural checks (duplicate IDs, dangling edges, start node protection) run before execution, but ideally would also run when the workflow is saved.
- **No self-terminating loops** — No node type mutates variables, so loops either exit on the first condition check or hit `maxExecutionSteps`. A counter/assignment node would fix this.
- **Global library mutation** — Changing a library node affects all workflows. A versioning or copy-on-write mechanism would prevent unintended side effects.
- **Save and delete are persistence-layer only (HTTP handlers out of scope)** — `UpsertWorkflow` and `DeleteWorkflow` are fully implemented and tested at the storage layer, but no HTTP endpoints expose them yet. The handlers were out of scope for this submission; the storage layer was prioritised because the three-tier data model makes persistence the complex part of these operations.

  - **`UpsertWorkflow`** — Single `READ COMMITTED` transaction that upserts the workflow header (`INSERT … ON CONFLICT DO UPDATE`, clearing `deleted_at` on re-save), then deletes and re-inserts all node instances (mapping node types to `node_library` IDs) and edges.
  - **`DeleteWorkflow`** — Single `READ COMMITTED` transaction that hard-deletes all edges and node instances, then soft-deletes the workflow header (`deleted_at` + `modified_at`). Returns `pgx.ErrNoRows` if the workflow does not exist.

  Both follow the same transactional pattern as `GetWorkflow` and would need only thin HTTP handlers to be exposed as `PUT /{id}` and `DELETE /{id}`.
- **No client-level tests** — The `pkg/clients/` packages (weather, flood) make real HTTP calls with no `httptest.Server` mocks. Node tests cover the integration boundary but the clients themselves are untested in isolation.

## What I'd Build Next

Ordered by impact, not by ease of implementation:

1. **Execution persistence** — Persist each run with its inputs, steps, and timing data. The `ExecutionResponse` struct is already the right shape for an `execution_runs` table. This is the highest-impact gap because without it there's no audit trail, no replay, and no way to debug failed workflows after the HTTP response is gone.

2. **Observability** — OpenTelemetry spans per node execution, Prometheus metrics, and structured log correlation. See [Observability](#observability) under Production Architecture for the full design — instrumentation points, span hierarchy, metric definitions, and the correlation gap between handler-layer request IDs and engine-layer logs.

3. **Save-time graph validation** — Currently structural checks run at execution time. Validating at save time (the `UpsertWorkflow` path) would prevent users from saving broken workflows. The `validateGraph` function already exists and is decoupled from execution — it just needs to be called from the save handler.

4. **Client-level tests** — The `pkg/clients/` HTTP clients have zero test coverage. `httptest.Server` mocks would catch timeout handling, error parsing, and malformed response edge cases that the node-level mocks don't exercise.

5. **Node versioning** — Pin workflows to specific library node versions via content-addressable metadata hashing or explicit version columns. This directly addresses the shared library mutation risk. The schema change is straightforward (add a `version` column to `node_library`, reference it from instances), but the migration path for existing data needs care.

6. **Parallel branch execution** — When independent branches exist in the graph (no data dependency), execute them concurrently with `errgroup`. The engine's adjacency list already supports multiple outgoing edges per node; the change is in the execution loop, not the data model.

### Production Architecture

Design ideas for scaling beyond the current synchronous single-process model. These are architectural directions, not planned implementations.

#### Workflow Triggers

Triggers are a separate concern from nodes, not start-node subtypes. The graph describes _what_ to do; triggers describe _when_ and _how_ to start. A workflow executes identically regardless of whether a human clicked "Run", a cron schedule fired, or a webhook arrived. `executeWorkflow` already accepts `(ctx, wf, inputs, deps)` — adding a new trigger type means adding a new _caller_, not changing the engine. The node graph stays clean; the start node remains a simple sentinel.

**`workflow_triggers` table** — Each row associates a trigger with a workflow. `trigger_type` is `schedule` or `webhook`. Schedule triggers store `cron_expression`, `timezone`, and a precomputed `next_trigger` timestamp. Webhook triggers store an opaque `webhook_token` (UNIQUE, URL-safe, revocable) and `secret_hash` for HMAC validation. Both types carry `default_inputs` (JSONB) for headless execution — scheduled workflows have no form submission, so the trigger supplies the inputs. Operational columns: `enabled`, `last_triggered_at`, `next_trigger_at`.

**Scheduled workflows** — A polling goroutine runs on a 30-second interval, querying for due triggers:

```sql
SELECT id, workflow_id, default_inputs
FROM workflow_triggers
WHERE trigger_type = 'schedule'
  AND enabled = true
  AND next_trigger_at <= NOW()
ORDER BY next_trigger_at
FOR UPDATE SKIP LOCKED
LIMIT 10;
```

`FOR UPDATE SKIP LOCKED` prevents double-firing when multiple instances poll concurrently — a row locked by one poller is invisible to others. After execution, the trigger's `next_trigger_at` is recomputed from the cron expression. Polling over an in-process cron library (like `robfig/cron`) because it's stateless — process restart doesn't lose schedule state, and the DB is the single source of truth.

**Webhook-triggered workflows** — New endpoint `POST /webhooks/{token}`. Token-based routing avoids exposing workflow IDs in external-facing URLs. The token is opaque, URL-safe, and revocable (delete the trigger row or set `enabled = false`). Inbound requests are validated via HMAC-SHA256: the caller signs the body with a shared secret, the server verifies against `secret_hash`. The webhook payload maps directly to workflow inputs. Sync execution initially — the response carries the full execution result, same as manual runs.

**Input flow by trigger type:**

| Trigger | Input source | Form validation |
| :--- | :--- | :--- |
| Manual (POST /execute) | Request body | Form node validates as normal |
| Schedule | `default_inputs` JSONB from trigger row | Form node validates identically |
| Webhook | Payload JSON from HTTP body | Form node validates identically |

The form node doesn't know or care where inputs came from. It validates the same `map[string]any` regardless of source.

**Gaps acknowledged:**
- No job persistence — a crash during a scheduled run loses the in-flight execution with no record it started
- No webhook payload mapping/transform — the payload must exactly match expected input field names
- No idempotency dedup for webhook retries — replayed webhooks execute the workflow again
- No rate limiting on the webhook endpoint

#### Sentinel Node Subtypes (End Nodes)

End-node subtypes handle what happens _after_ execution completes. Unlike start-node triggers (which are a separate concern, above), end sentinels do real work inside `Execute()`:

- **Report** — Generates a summary artifact from the execution context (e.g., PDF, Slack message)
- **Catalyst** — Enqueues a downstream workflow, enabling workflow-of-workflows composition
- **Janitor** — Runs cleanup logic (temp file removal, cache invalidation, external state teardown)

The current `end` node is a no-op sentinel. These subtypes extend it with meaningful terminal behaviour while keeping the graph structure unchanged.

**Async Execution with Workers** — Move from synchronous request-scoped execution to: HTTP request enqueues job → returns job ID → worker consumes and executes → persists results. This decouples the API latency from workflow duration and is a prerequisite for everything below. The current `executeWorkflow` function becomes the worker's inner loop, unchanged.

**In-Process Retries** — Node-level retry with backoff for transient failures (e.g., weather API timeout). Retry policy lives in node metadata (`maxAttempts`, `backoffMs`). Fits inside the current engine loop — the `Execute()` call gets wrapped in a retry — without requiring any infrastructure changes.

**DLQ + SSE** — After retry exhaustion, failed jobs land in a dead letter queue for manual inspection or automated reprocessing. Server-Sent Events push execution status to the frontend using session correlation and idempotency keys. Only makes sense once execution is async; in the current synchronous model the HTTP response already carries the full result.

#### Observability

Instrumentation design for traces, metrics, and log correlation. The codebase already has the foundation pieces in place — the gaps are correlation across layers and export infrastructure.

**What exists today:**

| Pillar | Status | Where |
|--------|--------|-------|
| Structured logging | In place | `slog.NewJSONHandler` with JSON output (`main.go:27-30`) |
| Request ID | In place, handler-scoped | `requestIDMiddleware` generates/reuses `X-Request-ID`, stores in context (`service.go:37-47`) |
| Per-node timing | In place | `StepResult.DurationMs` captures elapsed time per node (`engine.go:25-35`) |
| Distributed tracing | Not present | No OpenTelemetry dependencies (`go.mod`) |
| Metrics | Not present | No Prometheus client |

The foundation is there; the gaps are **correlation** (request ID doesn't flow into engine or client logs) and **export** (timing data exists but isn't exposed as metrics).

**Tracing with OpenTelemetry** — Three span layers, each nesting inside the parent:

```
[workflow.execute]                          ← workflow-level span
  ├─ [node.execute: form]                   ← per-node span
  ├─ [node.execute: weather]
  │    └─ [http.client: open-meteo]         ← external call span (auto-instrumented)
  ├─ [node.execute: condition]
  └─ [node.execute: email]
```

- **Workflow span** — Wraps `executeWorkflow` (`engine.go:59`). Attributes: `workflow.id`, `workflow.name`, `request.id`. Created before the engine loop starts; all child spans nest under it automatically via context propagation.
- **Per-node span** — Wraps each `node.Execute(ctx, nCtx)` call inside the engine loop (`engine.go:145-149`). Attributes: `node.id`, `node.type`, `node.label`, `node.status`, `node.duration_ms`. The timing code (`start := time.Now()`) already exists at line 145 — the span just wraps it.
- **HTTP client spans** — `otelhttp.NewTransport()` wrapping the `http.Client` in weather and flood clients. Automatic span creation for outbound HTTP with status code, URL, and duration. No manual instrumentation needed in client code — swap the transport at construction time in `main.go`.

Packages: `go.opentelemetry.io/otel`, `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp`, `go.opentelemetry.io/contrib/instrumentation/github.com/gorilla/mux/otelmux`.

**Metrics with Prometheus** — Four metrics covering the operational questions:

| Metric | Type | Labels | Question it answers |
|--------|------|--------|---------------------|
| `workflow_executions_total` | Counter | `workflow_id`, `status` | How often does each workflow run? What's the failure rate? |
| `workflow_execution_duration_seconds` | Histogram | `workflow_id` | How long do workflows take end-to-end? |
| `node_execution_duration_seconds` | Histogram | `node_type`, `status` | Which node types are slow? Which fail? |
| `external_api_requests_total` | Counter | `service`, `status_code` | Are external APIs healthy? |

Increment points map to existing code locations: workflow counter at `engine.go:189` (return with `"completed"` status), node histogram at `engine.go:149` (after `elapsed` is computed), API counter from `otelhttp` auto-instrumentation on the HTTP transport.

**Structured log correlation** — The missing link. Currently `requestId` appears in handler logs via the middleware (`service.go:37-47`) but is **not propagated** into engine logs, node execution, or client calls. There's a correlation blind spot between "request arrived" and "node X called weather API."

Fix: extract request ID from context in the engine loop and include it in all `slog` calls. Better: use `slog.With()` to create a child logger with `requestId` + `traceId` baked in, then pass it through the execution path. This correlates logs with traces in Grafana/Loki without changing individual log call sites.

```
Before: {"level":"DEBUG","msg":"calling weather API","url":"..."}
After:  {"level":"DEBUG","msg":"calling weather API","url":"...","requestId":"abc-123","traceId":"def-456","nodeId":"weather-api"}
```

**Infrastructure** — OTel collector sidecar exports spans to Jaeger or Grafana Tempo. Prometheus scrapes a `/metrics` endpoint exposed by the Go process. Grafana dashboards tie traces, metrics, and logs together via `traceId` correlation. This is standard plumbing — the interesting decisions are _where spans go_ in the code, not where they're exported.

**What this doesn't solve** — No business-level alerting (e.g., "workflow X hasn't run in 24 hours"), no SLO tracking, no cost attribution per workflow. These need execution persistence first (item #1 in "What I'd Build Next") to have historical data to alert against.

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
| `services/workflow` | `engine_test.go` | Graph validation, while-loop execution, branching, cancellation, partial failure |
| `services/workflow` | `workflow_test.go` | HTTP handlers (GET, POST, 404, 400, 500) |

**Why table-driven**: Every test function uses the `[]struct{ name; input; want }` pattern with `t.Run` subtests. This makes it trivial to add new cases (e.g., adding a "custom variable" condition test is one struct literal, not a new function) and produces clear output showing exactly which case failed.

**Why parallel**: All tests call `t.Parallel()` at both the top-level and subtest level. This catches accidental shared state — if two subtests mutate the same variable, the race detector flags it immediately.

**Why pgxmock over testcontainers**: Storage tests mock the database via `pgxmock.PgxPoolIface`. This keeps tests fast and dependency-free (no running Postgres required), at the cost of not catching real query issues. The trade-off is intentional — integration tests against a real database would be the next layer to add, but unit tests with exact query matching catch the most common bugs (wrong column order, missing WHERE clauses).

**Why mock interfaces, not mock frameworks**: Node tests use hand-written mocks (e.g., `mockWeatherClient`) rather than generated mocks. For interfaces with 1-2 methods, a 5-line struct is simpler and more readable than pulling in `gomock` or `mockery`. The mock is right there in the test file.
