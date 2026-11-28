# ATE Kit Assistant Cutover & Acceptance Runbook

Use this runbook to flip Kit-based assistants (`runtime_version = 3`) onto the
ATE backend in dev (or any environment), verify end-to-end behavior after the
flip, record cold-start and resume latency, and roll back cleanly if needed.

Never paste customer identifiers, organization IDs, project IDs, emails, or
credentials into tickets, commits, or PR comments. Replace every `<PLACEHOLDER>`
locally at execution time.

## 1. Pre-flight & Backend Flip

There is no per-assistant backend selector: ATE is selected when
`GRAM_ASSISTANT_ATE_ENABLED=true` and `GRAM_ASSISTANT_RUNTIME_PROVIDER` is not
`disabled`; in that case, every Kit assistant (`runtime_version = 3`) in the
environment runs on ATE.

Verify the required environment variables are set on both the `server` and
`worker` deployments:

- `GRAM_ASSISTANT_ATE_ENABLED=true`
- `GRAM_ASSISTANT_ATE_IMAGE=<DIGEST_PINNED_ATE_IMAGE>` (e.g. `gcr.io/<PROJECT>/ate-runtime@sha256:<64_HEX>`)
- `GRAM_ASSISTANT_ATE_WORKER_SELECTOR=<KEY=VALUE>` (e.g. `workload-type=ate-assistants`)
- `GRAM_ASSISTANT_ATE_CRED_PROVIDER_SERVICE_ACCOUNT=<CRED_PROVIDER_GCP_SA_EMAIL>` (GCP service account email of the `gram ate-credential-provider` deployment in `ate-system`, authenticating via GKE Workload Identity Google OIDC ID tokens)
- `GRAM_ASSISTANT_ATE_API_ADDRESS=<ATE_API_HOST:PORT>`
- `GRAM_ASSISTANT_ATE_ROUTER_ADDRESS=<ATE_ROUTER_HOST:PORT>`
- `GRAM_ASSISTANT_ATE_API_SERVER_NAME=<OPTIONAL_TLS_SERVER_NAME>`
- `GRAM_ASSISTANT_ATE_ROUTER_SERVER_NAME=<OPTIONAL_TLS_SERVER_NAME>`
- `GRAM_ASSISTANT_ATE_SERVICE_CA_FILE=<PATH_TO_SERVICE_CA_PEM>`
- `GRAM_ASSISTANT_ATE_CLIENT_CERT_FILE=<PATH_TO_ROUTER_CLIENT_CERT_PEM>`
- `GRAM_ASSISTANT_ATE_CLIENT_KEY_FILE=<PATH_TO_ROUTER_CLIENT_KEY_PEM>`
- `GRAM_ASSISTANT_ATE_AUDIENCE_JWT_FILE=<PATH_TO_PROJECTED_SA_JWT>`
- `GRAM_ASSISTANT_ATE_ATESPACE=<PRECREATED_ATESPACE>`
- `GRAM_ASSISTANT_ATE_TEMPLATE=<TEMPLATE_BASE_NAME>`

Keep the previous GKE Kit flags configured during the initial transition window
so the switch reaper (`ReapStoppedAssistantRuntimes` / `sweepIdleKitRuntimes`)
can tear down retired GKE claims and revoke their workload admissions cleanly.

After rolling `server` and `worker`:

1. Active Kit assistants converge onto ATE on their next turn (their old
   `backend = 'gke'` runtime row transitions to `switch_retiring` with
   `ended_at` set, freeing `assistant_runtimes_v2_one_per_assistant`, and a new
   `backend = 'ate'` row is reserved immediately; threads start fresh Kit
   sessions).
2. Idle Kit assistants that receive no new turns are retired automatically by
   the existing switch reaper (`ReapStoppedAssistantRuntimes` on the hourly
   `AssistantRuntimeJanitorWorkflow` and `sweepIdleKitRuntimes` on
   `ReapStuckRuntimes`).

Confirm convergence in SQL (read-only) once the sweep completes:

```sql
SELECT backend, state, ended, count(*)
FROM assistant_runtimes
WHERE runtime_version = 3 AND deleted IS FALSE
GROUP BY 1, 2, 3
ORDER BY 1, 2, 3;
```

Expected after all turns settle: zero rows with `backend = 'gke' AND ended IS FALSE`. A remaining live GKE row may be an assistant with in-flight turns or unsettled wakes; re-run the query after it quiesces.

## 2. Acceptance Exercise

Use an internal test project (`<PROJECT_SLUG>`) and a Kit assistant
(`<ASSISTANT_ID>`, `runtime_version = 3`) with an active Project Gateway and at
least one granted MCP tool. Drive turns via the `assistants-dev` MCP server
(`run_turn`, `load_chat`) or the Speakeasy dashboard/CLI.

### 2.1 Separate Actors and Isolated Workspaces for Two Threads

1. Start **Thread A** by omitting `chat_id` from `run_turn`, save its returned
   `chat_id` (`<CHAT_ID_A>`) for follow-up turns, and ask the assistant to write
   a unique marker file inside its workspace:
   - Prompt: `Create /home/kit/workspace/thread-a.txt containing "from-thread-a" and confirm.`
2. Start **Thread B** on the same assistant by omitting `chat_id` from a second
   `run_turn` call (saving `<CHAT_ID_B>`):
   - Prompt: `Check whether /home/kit/workspace/thread-a.txt exists, then create /home/kit/workspace/thread-b.txt containing "from-thread-b".`
3. Verify Thread B reports `/home/kit/workspace/thread-a.txt` does **not** exist.
4. Confirm two distinct live actor rows exist in Postgres for the same runtime:

```sql
SELECT assistant_thread_id, actor_name, actor_uid, state, configured
FROM assistant_ate_actors
WHERE project_id = '<PROJECT_ID>'
  AND assistant_id = '<ASSISTANT_ID>'
  AND deleted IS FALSE
ORDER BY created_at;
```

Expected: two rows with distinct `actor_name` and `actor_uid`, both `state = 'running'` and `configured = true`.

### 2.2 Inference, Project Gateway Tool Call, and Zero Sandbox Credentials

1. In Thread A, invoke a granted MCP tool via the Project Gateway:
   - Prompt: `List the available MCP servers on the Project Gateway and run <GRANTED_TOOL_NAME>.`
2. Confirm both the model completion and the upstream MCP tool call succeed in
   the transcript (`load_chat`).
3. Inspect Thread A's running actor via `ate` / worker exec (read-only) to prove
   no Speakeasy credential exists inside the sandbox:

```sh
# Confirm mcp.json carries only the fixed placeholder Authorization header
# and zero workload_identity / tokenFile / Speakeasy secrets:
ate actor exec --atespace <ATESPACE> <ACTOR_NAME_A> -- \
  cat /home/kit/.kit/credentials/mcp.json

# Confirm config.toml uses provider = "openrouter" with no stored credentials:
ate actor exec --atespace <ATESPACE> <ACTOR_NAME_A> -- \
  cat /home/kit/.kit/config.toml

# Confirm process env carries only OPENROUTER_BASE_URL and the placeholder
# OPENROUTER_API_KEY (never a JWT or API key):
ate actor exec --atespace <ATESPACE> <ACTOR_NAME_A> -- \
  sh -c 'tr "\0" "\n" < /proc/$(pgrep -f "kit serve")/environ | grep -E "OPENROUTER|GRAM|SSL_CERT"'
```

Expected:

- `mcp.json` contains `"Authorization": "Bearer ate-egress-injected-placeholder"` and no `auth` block.
- `OPENROUTER_API_KEY=ate-egress-injected-placeholder` and `OPENROUTER_BASE_URL=<SERVER_URL>/projects/<PROJECT_ID>/chat/completions`.
- `SSL_CERT_FILE=/run/ate/egress-trust-bundle/trust-bundle.pem` (and the file exists on disk).

### 2.3 Suspend and Wake (Including Across Worker Nodes)

1. Wait for the idle warm window (`ateActorWarmWindow`, 10 minutes) or trigger a
   worker drain (`kubectl cordon <WORKER_NODE>` + evict the worker pod hosting
   `<ACTOR_NAME_A>`).
2. Confirm `<ACTOR_NAME_A>` transitions to `state = 'suspended'` in
   `assistant_ate_actors` and has a completed durable rootfs snapshot in ATE:

```sh
ate actor get --atespace <ATESPACE> <ACTOR_NAME_A>
```

3. With `<WORKER_NODE>` cordoned so ATE schedules the resumed actor onto a
   different worker node, send a follow-up message on Thread A:
   - Prompt: `Read /home/kit/workspace/thread-a.txt and return its exact contents.`
4. Verify the assistant returns `"from-thread-a"`, the actor is `running` on the
   new worker node, and `actor_uid` is unchanged.
5. Uncordon `<WORKER_NODE>` when done (`kubectl uncordon <WORKER_NODE>`).

### 2.4 Cancel a Running Turn

1. Send a slow prompt on Thread A (for example: `Sleep 30 seconds using shell, then reply done.`).
2. While the turn is running, cancel/interrupt the thread via the dashboard stop
   button or the `interrupt` API.
3. Confirm the running turn stops promptly and a subsequent prompt on Thread A
   (`Reply with "recovered"`) succeeds normally on the same actor.

### 2.5 Public Web Access vs Private / Metadata Blocking

1. In Thread A, verify public HTTPS access succeeds:
   - Prompt: `Fetch https://example.com over HTTPS and report the HTTP status code and page title.`
   - Expected: HTTP 200 and title returned.
2. In Thread A, verify private RFC1918, loopback, and cloud metadata endpoints
   are refused by ATE egress:
   - Prompt: `Attempt to curl http://169.254.169.254/computeMetadata/v1/, https://10.0.0.1/, and https://127.0.0.1:8080/ with a 5s timeout and report the curl exit codes/errors.`
   - Expected: every request is refused/blocked (never returns metadata or internal responses).

### 2.6 Cross-Thread Actor Isolation

1. From Thread A, attempt to contact Thread B's actor hostname or internal ports:
   - Prompt: `Attempt to reach https://<ACTOR_NAME_B>.actor.ate.dev:8081/acp and http://<ACTOR_NAME_B>:8082/status with a 5s timeout and report the result.`
   - Expected: refused/unreachable.

### 2.7 Record Cold-Start and Resume Durations

`KitATERuntimeBackend.prepare` records both an OpenTelemetry histogram and a
structured `INFO` log on every actor readiness transition:

- **Metric (Datadog / OTel)**:
  - Name: `assistant.ate.actor.ready_duration_ms` (unit: `ms`)
  - Attribute/Tag: `gram.assistant.ate.startup_mode:cold_start` (actor `CreateActor` -> `/readyz` -> `/bootstrap` -> `/app-readyz`)
  - Attribute/Tag: `gram.assistant.ate.startup_mode:resume` (`ResumeActor` -> `/readyz` -> `/app-readyz`)
  - Attribute/Tag: `gram.assistant.ate.startup_mode:warm` (already-running actor readiness check)
- **Structured Log (`server` / `worker` logs)**:
  - Message: `"ATE thread actor ready"`
  - Fields: `gram.assistant.id`, `gram.project.id`, `gram.assistant.runtime_id`, `gram.assistant.thread_id`, `gram.assistant.ate.actor_name`, `gram.assistant.ate.actor_uid`, `gram.assistant.ate.startup_mode`, `gram.assistant.ate.ready_duration_ms`, `gram.assistant.ate.create_to_ready_ms` (cold start), `gram.assistant.ate.resume_to_ready_ms` (resume).

Record p50/p95 `gram.assistant.ate.create_to_ready_ms` and `gram.assistant.ate.resume_to_ready_ms` from the acceptance
exercise in the cutover ticket (without tenant-identifying fields).

## 3. Rollback

To roll back to GKE Kit without manual DB edits:

1. Set `GRAM_ASSISTANT_ATE_ENABLED=false` on `server` and `worker` while leaving
   the `GRAM_ASSISTANT_ATE_*` connection/image env vars present during the
   drain window (this attaches ATE as a retiring-only backend).
2. Roll `server` and `worker`.
3. Active assistants converge back to `backend = 'gke'` on their next turn;
   idle ATE assistants are retired automatically by `ReapStoppedAssistantRuntimes`
   and `sweepIdleKitRuntimes`.
4. Confirm in Postgres that zero live ATE runtimes or actors remain, and that
   retired ATE actor subjects have been removed from `workload_identity_admissions`:

```sql
SELECT count(*) AS live_ate_runtimes
FROM assistant_runtimes
WHERE runtime_version = 3 AND backend = 'ate' AND deleted IS FALSE AND ended IS FALSE;

SELECT count(*) AS live_ate_actors
FROM assistant_ate_actors
WHERE deleted IS FALSE;

-- Confirm retired actor subjects (for example actor/<ATESPACE>/<ACTOR_NAME_A>)
-- are no longer admitted in the test project:
SELECT count(*) AS remaining_ate_admissions
FROM workload_identity_admissions
WHERE project_id = '<PROJECT_ID>'
  AND subject IN ('actor/<ATESPACE>/<ACTOR_NAME_A>', 'actor/<ATESPACE>/<ACTOR_NAME_B>');
```

All three counts converge to `0` automatically.
