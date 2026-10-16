# Credential upstream OAuth client selection backfill

`upstream_client_selections` records, for each inbound credential and MCP server, the upstream OAuth client that credential uses, or an explicit choice that the server needs none. Once serving reads it, a credential keeps the upstream identity it was issued with, independent of how its user session issuer's bindings change later. Until then, sign-in still resolves through the issuer's bindings, and these rows are written but not read.

This tool records choices for existing credentials and reports the pairs configuration does not decide. It changes no schema and runs no Atlas migration. Run it after the MCP server association backfill (`UPSTREAM_CLIENTS_BACKFILL.md`) reports `gaps=0`, since choices are made from associations.

## Credential references

| Kind           | Reference                                   | Lifetime                                                                                                                                                                                                                                            |
| -------------- | ------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `user_session` | `COALESCE(user_sessions.credential_id, id)` | From the authorization that created the session until its refresh deadline. Refresh rotation mints a new row and copies the reference, so every row of one credential shares it. A row minted before `credential_id` existed is its own credential. |

The reference names a record, never a token, assertion or secret, and grants nothing by itself: the credential is admitted on its own path before a choice is read. User, agent and workload sessions issued by the MCP token endpoint, inbound ID-JAG sessions and dashboard playground sessions are all `user_session` credentials.

**Out of scope, unchanged:** credentials with no durable record keep resolving through the issuer path: principal credentials, assistant tokens, chat sessions and API keys (including agent keys and their session attachments). Hosted toolsets keep resolving every token their issuer binds.

## How a choice is made

A choice is recorded when a credential is issued, in the same transaction as its session row, and never on a request. Refreshing the credential, or refreshing or reconnecting its upstream connection within the same client, changes nothing, and nothing pins a token or grant generation. A choice is never replaced.

For each server the credential can reach (servers on its issuer, members of a gateway signing in through it, and every server in the organization for an organization issuer):

| Outcome              | Evidence                                                                            | Recorded                   |
| -------------------- | ----------------------------------------------------------------------------------- | -------------------------- |
| `client`             | Exactly one of the server's associated clients is bound to the credential's issuer. | That client                |
| `no_auth`            | The server has no associated client and its own issuer binds none.                  | Explicit no-auth           |
| `ambiguous`          | Several associated clients are bound to the credential's issuer.                    | Nothing; reported as a gap |
| `unreachable_client` | The server has associated clients, but none is bound to the credential's issuer.    | Nothing; reported as a gap |
| `unassociated`       | The server has no associated client although its issuer binds some.                 | Nothing; reported as a gap |

A missing choice is never the same as no-auth, and nothing infers one at first use.

A chosen client that is later detached from the server or deleted leaves the choice in place but unusable (`GetUpstreamClientSelection` reports `usable=false`). Serving then fails closed and asks the user to reconnect; it never switches to another client.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations credential-upstream-selections \
  -environment=staging \
  -report=/tmp/upstream-selections-preview.json
```

Stdout carries counts by credential class. The `-report` file lists each gap's credential and server ids. Add `-organization=<id>` to scope a run.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations credential-upstream-selections \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=credential-upstream-selections \
  -report=/tmp/upstream-selections-apply.json
```

Writes go through the same statement issuance uses, so an issuance racing the backfill records the same choice and the loser is a no-op. The run is idempotent; rerun the same command after an error.

## Coverage gate and repair

New credentials record their choices at issuance from the moment this ships, so the window between backfill and cutover is covered as it happens. Rerun the backfill to pick up anything configuration changes resolve later. Before cutover:

1. Apply the backfill, then rerun the preview.
2. Resolve each gap:
   - `unassociated`: configure User Identity on the server (association backfill runbook), then rerun.
   - `ambiguous` or `unreachable_client`: the credential must reconnect, and its new credential records its choice at issuance. Before cutover, revoke the listed credentials' sessions to force it; after cutover, serving asks for reconnection itself.
3. Cutover is blocked while `gaps` is non-zero for credentials expected to keep working.

## Cutover

Serving switches per organization with the PostHog flag `upstream-client-selection-serving`, targeted by organization group. Turn it on for an organization only once both backfills report no gaps for it. While it is on, for that organization:

- A direct remote or tunneled server forwards the caller's credential from the client the caller's credential chose for it, and routes it by that client's own issuer. A missing or unusable choice answers with the reconnect challenge; a no-auth choice calls anonymously.
- A gateway resolves each proxied member the same way, per member. A member without a usable choice reports that the gateway must be signed in to again; the other members keep serving.
- Consent offers a server only the clients associated with it, and a gateway only the clients associated with the members the person reaches. Gateway member status, the consent verification probe and the Platform MCP connection status tool match clients by association.

Hosted toolsets and credentials without a session row (principal credentials, agent keys, assistant tokens) keep the issuer-derived path either way.

**Rollback** is turning the flag off for the organization: serving reads the issuer bindings and `mcp_servers.remote_session_issuer_id` again, which the binding writes keep maintaining. Choices keep being recorded at issuance while the flag is off, so turning it back on needs no new backfill. The rollback stops being available once the issuer-derived path is removed. That removal happens only after the flag has been on for every organization and nothing reads the derived column any more, including the excluded paths above.
