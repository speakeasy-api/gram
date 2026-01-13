# Gram Functions

Gram Functions is a serverless code execution feature that allows users to deploy custom JavaScript/TypeScript or Python code as callable tools within Gram deployments. Functions can be invoked by AI agents during conversations.

## Key Server Packages

| Package                                  | Purpose                                                    |
| ---------------------------------------- | ---------------------------------------------------------- |
| `server/internal/functions/`             | Core functions service - deployment, execution, auth       |
| `server/internal/background/activities/` | Temporal activities for deploying/reaping function runners |
| `server/design/functions/`               | Goa API design for functions endpoints                     |

## Key Files

### Server Implementation (`server/internal/functions/`)

- **`impl.go`** - API service, handles signed asset URL requests from runners
- **`deploy.go`** - Core interfaces: `Deployer`, `ToolCaller`, `Orchestrator`
- **`deploy_fly.go`** - Fly.io integration via Machines API
- **`manifest.go`** - Manifest format (`ManifestV0`) describing exported tools/resources
- **`runtimes.go`** - Supported runtime definitions (JS, TS, Python)
- **`auth.go`** - JWT authentication for function runners
- **`queries.sql`** - SQL queries for `fly_apps` table management

### Background Workers (`server/internal/background/`)

- **`activities/deploy_function_runners.go`** - Deploys runners during deployment processing
- **`activities/reap_functions.go`** - Cleans up old Fly.io apps
- **`activities/provision_functions_access.go`** - Creates access credentials for runners
- **`functions_reaper.go`** - Temporal workflow for cleanup (triggered per-project after deployments)

## Function Runner OCI Images (`functions/`)

The `functions/` directory contains the source code and build configuration for the OCI images that run user functions on Fly.io. These images are built using [melange](https://github.com/chainguard-dev/melange) and [apko](https://github.com/chainguard-dev/apko) for reproducible, minimal container images.

### Build System

| File                                | Purpose                                              |
| ----------------------------------- | ---------------------------------------------------- |
| `melange.yaml`                      | Builds the `gram-runner` Go binary as an APK package |
| `images/nodejs22-alpine3.22.yaml`   | apko config for Node.js 22 runtime image             |
| `images/python3.12-alpine3.22.yaml` | apko config for Python 3.12 runtime image            |

### Image Structure

Each runtime image contains:

- Alpine Linux base with minimal packages (`ca-certificates-bundle`, `su-exec`)
- Language runtime (`nodejs` or `python3`)
- The `gram-runner` binary (built from `cmd/runner/main.go`)

**Entrypoint behavior:**

1. `gram-runner -init -language <lang>` - Initializes filesystem, unzips user code
2. `su-exec gram` - Drops privileges to non-root `gram` user (UID 10000)
3. `gram-runner -language <lang>` - Starts HTTP server on port 8888

### Runner Binary (`cmd/runner/main.go`)

The `gram-runner` binary is an HTTP server that:

- Listens on `:8888` for tool call and resource requests
- Authenticates requests using JWT tokens
- Spawns language-specific subprocesses to execute user code
- Communicates with subprocesses via named pipes (FIFO)
- Reports resource usage (CPU, memory, execution time) as HTTP trailers
- Auto-terminates after 1 minute of idle time (scale-to-zero support)

### Internal Packages (`functions/internal/`)

| Package       | Purpose                                                                     |
| ------------- | --------------------------------------------------------------------------- |
| `auth/`       | JWT authentication and request authorization middleware                     |
| `bootstrap/`  | Machine initialization: unzip code, prepare entrypoints, lazy asset loading |
| `encryption/` | AES-GCM encryption for secure communication                                 |
| `guardian/`   | Process execution with resource limits                                      |
| `ipc/`        | Named pipe (FIFO) creation for subprocess communication                     |
| `javascript/` | JavaScript/TypeScript entrypoint script (`gram-start.js`)                   |
| `python/`     | Python entrypoint script (`gram_start.py`)                                  |
| `runner/`     | HTTP handlers for `/tool-call` and `/resource-request` endpoints            |
| `svc/`        | Service utilities (idle tracking, secrets, errors)                          |
| `o11y/`       | Observability setup (OpenTelemetry, logging)                                |
| `middleware/` | HTTP middleware (recovery, version header)                                  |
| `attr/`       | Structured logging attribute helpers                                        |

### Tool Call Execution Flow

1. **Request received** at `POST /tool-call` with JSON payload:
   ```json
   {"name": "tool_name", "input": {...}, "environment": {...}}
   ```
2. **FIFO created** - Named pipe for IPC with subprocess
3. **Subprocess spawned** - `node --experimental-strip-types gram-start.js` or `python gram_start.py`
4. **Arguments passed** - FIFO path, serialized request, request type ("tool" or "resource")
5. **Response read** - HTTP response format read from FIFO
6. **Metrics collected** - CPU time, memory, execution duration added as trailers
7. **Cleanup** - FIFO removed, subprocess waited

### Lazy Asset Loading

For large function bundles (>700KiB), the code isn't embedded in the Fly machine config. Instead:

1. A `.lazy` file is written containing the asset ID
2. On init, `bootstrap.resolveLazyFile()` detects the `.lazy` file
3. Runner fetches a pre-signed URL from the Gram server
4. Code is downloaded from Tigris blob storage and unzipped

## Database Tables

| Table                           | Purpose                                                       |
| ------------------------------- | ------------------------------------------------------------- |
| `deployments_functions`         | Links functions to deployments, stores runtime/slug           |
| `functions_access`              | Encryption keys and bearer token formats for auth             |
| `fly_apps`                      | Tracks deployed Fly.io apps (status, region, URL, reap state) |
| `function_tool_definitions`     | Tool metadata (name, description, input schema, variables)    |
| `function_resource_definitions` | Resource metadata (URI, mime type)                            |

## Deployment Flow

1. **Upload** - User uploads ZIP archive with function code + `manifest.json`
2. **Processing** - Temporal workflow triggers `DeployFunctionRunners` activity
3. **Fly.io Deployment** - Creates Fly app via Machines API with appropriate runtime image
4. **Asset Handling** - Small functions embedded in config; large functions use Tigris blob storage with lazy loading
5. **Auto-scaling** - 2 machines per function, scale to 0 when idle

## Execution Flow

1. AI agent requests tool call → `FlyRunner.ToolCall()` sends authenticated HTTP request
2. Runner receives request at `/tool-call` endpoint
3. Subprocess spawned (`node`/`python`) with user code
4. Communication via named pipe (FIFO)
5. Response streamed back with resource usage metrics as HTTP trailers

## Cleanup (Reaping)

- Functions Reaper workflow is triggered after each deployment completes (see `server/internal/deployments/impl.go`)
- Keeps only N most recent deployments' Fly apps per project (default: 3)
- Old apps deleted via Machines API, marked `reaped_at` in database
- Note: A `FunctionsReaperScopeGlobal` scope exists in the code but is not currently used/scheduled
