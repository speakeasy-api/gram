# MCP server upstream authorization server backfill

`mcp_servers.remote_session_issuer_id` is the upstream authorization server an MCP server's users sign in to. It used to be derived per user session issuer: the single provider among the clients bound to that issuer was written onto every server on it, and two providers blanked them all. Once an organization-level user session issuer could be shared by many servers, that derivation borrowed one server's OAuth upstream for its siblings or left them all with no sign-in.

The column is now an explicit per-server fact written when an administrator configures User Identity on a server. The post-commit reconcile keeps that value while a client for it is bound, and derives one only for a server alone on its user session issuer — which has no sibling to borrow from, and is the only configuration a toolset-backed or tunneled server has. A borrowed value on a shared issuer still has a live client, so the reconcile keeps it: this offline backfill is what repairs the values the old derivation left behind. It changes no schema and runs no Atlas migration.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only: the rows it decides name customer projects and upstreams.

## What it decides

For every live MCP server carrying a user session issuer:

1. **Metadata match wins.** The server's upstream URL names a protected resource (`remote_protected_resources`, captured by RFC 9728 discovery) whose `authorization_servers` list the issuer URL of exactly one provider its user session issuer binds a client for. That provider is the server's own upstream, whatever its siblings use, and it is stamped. Resource identifiers are matched exactly, as the serve-time lookup does, because `/mcp` and `/mcp/` may be different resources. `remote_session_clients.resource_identifier` is deliberately unused: most clients leave it unset.
2. **A lone server on its issuer is left alone.** With one server on the user session issuer the old derivation could not have borrowed anything, so the stored value is trustworthy — and the reconcile keeps deriving it there anyway. This is the only evidence available for a hosted (toolset-backed) server, which has no upstream URL and no protected resource metadata.
3. **Everything else is cleared.** The stored value came from an issuer shared by several servers, so it may be a sibling's provider. NULL is correct for a server that works anonymously while still advertising OAuth metadata, and readers fail closed on it rather than routing a bearer to the wrong upstream.

A cleared server shows as needing sign-in configuration again; an administrator reconfigures User Identity on it, which writes the column directly.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations server-remote-issuers \
  -environment=staging
```

The default mode reads and decides without writing. `stamped`, `cleared` and `kept` are the prospective counts.

Add `-organization=<id>` to scope a run to one organization, for a staged rollout.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations server-remote-issuers \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=server-remote-issuers
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx, which ties write confirmation to the actual database target rather than the descriptive environment label.

Each write is the same statement the identity commit uses, so a server that has moved off the user session issuer the decision was made against is skipped rather than stamped. The operation is idempotent: a rerun reports `stamped=0 cleared=0`. Stop on any error, investigate, then rerun the same command.

## Ordering

Run it after the per-server writes are deployed. Before then the old derivation still recomputes the column per issuer and would undo the backfill at the next client binding change.
