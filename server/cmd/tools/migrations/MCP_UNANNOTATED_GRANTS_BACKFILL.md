# MCP unannotated connect rule backfill

A tool that sets no annotation hint now has a disposition of its own, `unannotated`. Before that change, such a tool call carried no disposition, and a missing disposition matched a rule or block limited to any disposition. So until now:

- a connect rule limited to `read_only` (or any other disposition) also let callers use the server's tools without annotations;
- a connect block limited to `destructive` (or any other disposition) also blocked them.

Once the change is deployed, neither applies: a disposition-limited rule or block only reaches tools with that disposition. This one-time application-data backfill keeps the access each stored row gave before. Beside every connect rule or connect block limited to a disposition other than `unannotated`, it stores the same rule or block limited to `unannotated`. It does not alter the schema or Atlas migrations.

For every principal that holds such a row, in one transaction per principal:

- the twin (the same scope and selector, with `disposition` set to `unannotated`) is inserted with the limited row's expiry;
- when the principal already holds the twin, it keeps whichever expiry lasts longer (no expiry means permanent).

Expired rows are skipped. Limited rows are never changed or deleted. The principal's limited rows are locked for the transaction, and role principals also take the lock role edits take. Rerunning finds nothing left to do. No audit entry is written: the backfill restores access principals already had.

Admins can remove a twin afterwards like any other rule, to stop tools without annotations from being reached under a `read_only` rule.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Rollout

Run the apply right after the deploy that gives tools without annotations their own disposition. It cannot run earlier: older servers refuse `unannotated` as a disposition, so they could not load or edit the twins.

Between that deploy and the apply, tools without annotations are refused under disposition-limited connect rules and are not blocked by disposition-limited connect blocks. Keep that window short: have the preview output ready before the deploy and run the apply as soon as the deploy finishes.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations mcp-unannotated-grants \
  -environment=staging
```

Every transaction is rolled back. `principals`, `added` and `extended` are the prospective counts. Pass `-org=<ORG_ID>` to limit the run to one organization.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations mcp-unannotated-grants \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=mcp-unannotated-grants
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx. An apply also needs `sslmode=require`, `verify-ca` or `verify-full` in `GRAM_DATABASE_URL`. If an apply fails part way, it prints the counts already written before the failure. Stop on any error, investigate, then rerun the same command. A subsequent preview should report `principals=0 added=0 extended=0`.
