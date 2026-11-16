# MCP view and manage rule limit rewrite

A tool or annotation limit is saved on MCP connect rules and connect blocks only. Rules for viewing (`mcp:read`) or managing (`mcp:write`) a server, and blocks on either, used to accept one as well, and some stored rows still carry it. This one-time application-data rewrite stores those rows in the form they already take effect as. It does not alter the schema or Atlas migrations.

No check for viewing or managing a server names a tool, so the limit never did anything:

- a limited view or manage rule matched every such check, exactly as the same rule without a limit does;
- a limited block on view or manage never matched a check.

`authz.StoredSelectorInEffect` already returns stored rows in that form when they are loaded, so no access decision depends on this rewrite and none changes when it runs. It makes the stored data match what is enforced, so the load-time handling can be removed afterwards.

For every principal that holds such a row, in one transaction per principal:

- a limited view or manage rule is stored again without its `tool` or `disposition` key;
- when the principal already holds the same rule without a limit, the limited row is deleted instead, and the remaining row keeps whichever expiry lasts longer (no expiry means permanent);
- a limited block on view or manage is deleted.

Connect rules and connect blocks are never touched. The principal's limited rows are locked for the transaction, and role principals also take the lock role edits take. Rerunning finds nothing left to do. No audit entry is written, because no principal gains or loses access.

Delegated policies already issued to agents are stored elsewhere and are not rewritten. They keep decoding as before.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Rollout

Deploy the server change that refuses new limits on view and manage rules first, so no new row of this kind can be written. Then run the preview, then the apply, against each environment. The order is not critical for access: until the apply runs, the rows keep working as they do today.

Run the apply only after the MCP connect backfill (`BackfillMcpConnectGrants` in the admin API) has run for the last time in that environment. That backfill copies each view or manage rule to a connect rule with the same selector, limit included. Once this rewrite has removed a limit, a later backfill run would copy the rule as an unlimited connect rule and widen access.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations mcp-grant-limits \
  -environment=staging
```

Every transaction is rolled back. `principals`, `limits_removed`, `merged` and `blocks_removed` are the prospective counts. Pass `-org=<ORG_ID>` to limit the run to one organization.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations mcp-grant-limits \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=mcp-grant-limits
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx. Stop on any error, investigate, then rerun the same command. A subsequent preview should report `principals=0 limits_removed=0 merged=0 blocks_removed=0`.
