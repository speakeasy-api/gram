# MCP server upstream OAuth client backfill

`mcp_server_remote_session_clients` records which upstream OAuth clients (remote session clients) each MCP server uses. One client can serve several servers, and one server can offer several clients. It replaces deriving a server's upstream from the clients bound to its user session issuer, which is wrong whenever several servers share one issuer: one server's OAuth upstream is applied to its siblings, or several upstreams leave every server with none.

Associations are written ahead of the serving path that will read them. Until that cutover, sign-in still resolves through the user session issuer's bindings, and associations are kept to what those bindings would serve:

- Configuring User Identity on a server (dashboard or Platform MCP) associates that server, and only that server, with the chosen client.
- When a server is the only consumer of its user session issuer, every client bound to the issuer is associated with it on each binding change.
- Unbinding a client from an issuer, deleting a client, deleting a server or removing its issuer removes the associations the bindings no longer serve.
- `remoteSessions.detachServerClientAssociation` removes one server's association on a shared issuer. A sole consumer's association follows its binding and is detached by turning off User Identity instead.

This tool records the associations existing configuration already proves and reports the servers whose configuration proves none. It changes no schema and runs no Atlas migration.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Stdout carries aggregate counts only; the per-server report, which holds server and client ids, goes to `-report`.

## What it decides

For every live MCP server carrying a user session issuer:

| Outcome          | Evidence                                                                                                                                                                             | Writes                   |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------ |
| `sole_consumer`  | The server is the only live server on its issuer and no gateway fronts it, so every client bound to the issuer is meant for it. This includes hosted servers with several upstreams. | Every bound client       |
| `metadata_match` | The issuer is shared, and the server's own upstream URL names a protected resource (RFC 9728, matched exactly) whose `authorization_servers` list exactly one bound client's issuer. | That client              |
| `configured`     | The issuer is shared, nothing proves a client, and the server already has an explicitly configured association.                                                                      | Nothing                  |
| `ambiguous`      | The issuer is shared and nothing proves a single client: no metadata, several matches, or a match an administrator has since detached.                                               | Nothing; reported as gap |
| `unbound`        | The issuer binds no client.                                                                                                                                                          | Nothing                  |

It never copies a shared issuer's clients onto every server on it, and never reads `unbound` or a NULL `mcp_servers.remote_session_issuer_id` as proof that the server needs no upstream authentication.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations mcp-server-upstream-clients \
  -environment=staging \
  -report=/tmp/upstream-clients-preview.json
```

Add `-organization=<id>` to scope a run to one organization.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations mcp-server-upstream-clients \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=mcp-server-upstream-clients \
  -report=/tmp/upstream-clients-apply.json
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL`. `-apply` refuses an existing `-report` file.

Each write is conditional on the evidence the run read: the server is still live on the same issuer, the client is still bound to it and in the server's tenant, and, on a shared issuer, no association row for the pair was recorded meanwhile. A configuration change between the read and the write wins, and the write is counted as `skipped`. The run is idempotent; rerun the same command after an error or to pick up skipped rows, with a new `-report` path since `-apply` refuses an existing one.

## Activation gate

Serving must not read associations until coverage is complete. Before cutover:

1. Deploy the association writes, so new configuration is captured as it happens.
2. Apply this backfill, then rerun it until `skipped=0`.
3. Resolve every `ambiguous` server in the report by configuring User Identity on it, which writes its association, and rerun until `gaps=0`.

Rerun the preview immediately before cutover. Any `gaps` or `skipped` count blocks activation.
