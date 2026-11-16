# Assistant agent names backfill (AIM-1040)

Repairs application data, not schema. Deploy the provisioning fix first so new
assistant agents no longer receive generated names.

The `assistant-agent-names` command copies the live bound assistant's current
`name` onto its agent **only** when the agent name exactly equals
`Assistant <assistant UUID>`. Custom names (including generated-name prefixes
with suffixes), deleted agents/assistants/bindings/projects, missing live
references, and cross-tenant/project associations are preserved. Suspended or
revoked but undeleted agents remain eligible: this changes display names only,
not credentials or lifecycle state. An already matching name is a no-op.

## Operation

Use the approved operator environment and supply `GRAM_DATABASE_URL` through
its secret mechanism. Never paste credentials into commands or reports.

Dry run is the default. Start scoped to one organization and project:

```sh
go run ./server/cmd/tools/migrations assistant-agent-names \
  -environment=production -org='<ORG_ID>' -project='<PROJECT_ID>'
```

Review the aggregate `agents` count, then explicitly confirm the environment,
parsed database target (host:port/database), and command:

```sh
go run ./server/cmd/tools/migrations assistant-agent-names \
  -environment=production -org='<ORG_ID>' -project='<PROJECT_ID>' \
  -apply -confirm-environment=production \
  -confirm-target='<DB_HOST>:<DB_PORT>/<DATABASE>' \
  -confirm-apply=assistant-agent-names
```

Omit `-project` and/or `-org` only when intentionally widening the scope.
Explicit empty scopes are rejected. Reports contain counts, not names or IDs.
Environment confirmation is an operator acknowledgement, not environment discovery;
target confirmation checks the parsed connection host, port, and database.
Cross-host connection fallbacks are rejected.

Each run is one atomic statement. Both dry run and apply briefly lock eligible
agents, assistants, bindings, and projects; dry run executes no row updates.
Apply rechecks the exact generated name and updates `agents.updated_at` with the
name. Concurrent edits can wait for these locks; run scoped during low traffic
and use operator-approved statement/lock timeouts in the connection settings.
There is no batching or automatic retry. Cancellation/failure is atomic, but a
lost connection after commit can make the outcome uncertain: rerun the dry run.
Rerunning apply is safe; repaired rows no longer qualify.

## Caveats

An intentional custom name that **exactly** equals the historical generated
name is indistinguishable from a generated name and will be replaced. Review
this policy before applying. No automatic rollback is provided because a later
custom rename must not be overwritten. This is a one-time repair, not ongoing
synchronization when an assistant is renamed.

## Tests

```sh
mise run test:server ./cmd/tools/migrations -run TestParseAssistantAgentNames
mise run test:server ./cmd/tools/migrations/assistantagentnames
```
