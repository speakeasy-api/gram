# Risk failure mode backfill

Prompt policies used to carry their own `fail_open` setting in `risk_policies.model_config`. A judge timeout or error now follows the organization's risk failure mode, the `risk_enforcement_fail_closed` product feature, and the server no longer reads the per-policy key.

This offline application-data backfill keeps every policy that chose fail closed failing closed. It turns `risk_enforcement_fail_closed` on for each organization with at least one live (not deleted) prompt-based policy whose stored `fail_open` is `false`, enabled or not. It writes through the same `EnableFeatureIfNeverConfigured` query the entitlement seeders use, so an organization that already has a row for the feature keeps it, and a soft-deleted row (an administrator turned fail closed off) is never restored. It does not alter the schema, Atlas migrations, or the stored `model_config` values.

Run from `server/`. `GRAM_DATABASE_URL` must point directly at the intended PostgreSQL database. Output contains aggregate counts only.

## Rollout

Run the apply as soon as the release that stops reading `fail_open` deploys; until then those policies fail open on a judge error. The feature row is inert for the previous release, so applying just before the deploy is also safe.

## Preview (default)

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations risk-fail-closed \
  -environment=staging
```

Every candidate is seeded in a transaction that is always rolled back, so `features_added` is the prospective count. No writes are committed.

## Apply

```sh
GRAM_DATABASE_URL=... go run ./cmd/tools/migrations risk-fail-closed \
  -apply \
  -environment=staging \
  -confirm-environment=staging \
  -confirm-target=db.example.internal:5432/gram \
  -confirm-apply=risk-fail-closed
```

`-confirm-target` must exactly match the host, port, and database parsed from `GRAM_DATABASE_URL` by pgx. Each organization is handled in its own transaction. The operation is idempotent: a rerun reports `features_added=0`. Stop on any error, investigate, then rerun the same command.

## Output

```
mode=apply environment=staging organizations=<N> mixed_organizations=<M> features_added=<K>
```

- `organizations`: organizations with a live prompt policy that stored `fail_open=false`.
- `mixed_organizations`: those that also have prompt policies that failed open. Once the organization fails closed, those policies fail closed too. This keeps every policy that chose fail closed from silently failing open; tell the affected organizations they can switch back to allowing from the "When the judge can't answer" card in any prompt policy's editor.
- `features_added`: organizations whose failure mode this run set, or in a dry run would set.

The dashboard's product feature cache may show the previous value for up to its normal TTL. Enforcement reads the setting uncached.
