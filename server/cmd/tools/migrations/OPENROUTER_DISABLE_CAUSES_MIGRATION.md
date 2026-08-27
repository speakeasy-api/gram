# OpenRouter disable-cause classifier

`openrouter-disable-causes` is a PostgreSQL-only operator migration. It classifies legacy `openrouter_api_keys` rows whose `disable_causes` is `NULL`; it never calls OpenRouter. Run it only from the published revision approved for the rollout.

## Safety model

- The default mode is dry-run. There is no `-dry-run` flag.
- `-apply` and `-manual-override` are the only writing modes. Every write requires `-confirm-environment` to exactly match `-environment`. Production writes additionally require `-confirm-production=production`.
- Automatic billing classification fails closed. `billing_inactive` requires a current `free` account with no subscription and a durable `organization:payg_deactivated` audit whose organization subject and snapshots prove a `payg` to `free` transition. Never-PAYG, malformed, and contradictory rows remain ambiguous.
- Admin evidence requires the production audit identity: subject ID `<ORG_ID>/<KEY_TYPE>` and subject type `openrouter_api_key`.
- Output is aggregate JSON only. Blocked logs expose only `ambiguous_rows`, `validation_failed`, `override_conflict`, `database_or_timeout`, or `unexpected`; they do not include row identifiers, override contents, credentials, or database URLs.

Set `GRAM_DATABASE_URL` in the environment. Do not put credentials in flags or shell history. Set `GRAM_CODE_SHA` to the published commit being run.

## Procedure

1. **Dry-run** and retain the aggregate output:

   ```sh
   go run ./server/cmd/tools/migrations openrouter-disable-causes \
     -environment=staging
   ```

2. Investigate every aggregate ambiguity category. Do not infer prior PAYG state from current `free` state. If durable audit evidence is absent, stop. The architecture choices are: leave the row for an authorized manual override, add a separately reviewed durable provenance source, or redesign the migration contract.

3. **Apply** only safe classifications:

   ```sh
   go run ./server/cmd/tools/migrations openrouter-disable-causes \
     -apply -environment=staging -confirm-environment=staging
   ```

   For production, use both confirmations:

   ```sh
   go run ./server/cmd/tools/migrations openrouter-disable-causes \
     -apply -environment=production -confirm-environment=production \
     -confirm-production=production
   ```

4. **Validate** the complete live population at one PostgreSQL snapshot:

   ```sh
   go run ./server/cmd/tools/migrations openrouter-disable-causes \
     -validate -environment=production
   ```

   Validation is non-writing. A nonzero exit is a stop condition, not a handoff signal.

5. If an authorized investigation establishes evidence unavailable to the classifier, provide one protected override JSON object on standard input. Set `GRAM_OPENROUTER_DISABLE_CAUSES_OVERRIDE_TOKEN` out of band and use all write confirmations. Never paste identifiers, tokens, or manifests into tickets, commits, or logs.

## Retries and recovery

Lock and statement timeouts are bounded by `-lock-timeout`, `-statement-timeout`, and `-max-lock-retries`. A blocked run is safe to retry after correcting the reported aggregate category: writes are batched, compare-and-set, resumable, and idempotent. Do not increase timeouts until database contention or query failure has been identified.

If the category is `database_or_timeout`, verify connectivity and database health without printing `GRAM_DATABASE_URL`. For `ambiguous_rows` or `validation_failed`, inspect aggregate counts and use privacy-preserving database queries; do not export row-level data.

## Rollout order

1. Deploy the nullable compatibility schema and application reads/writes.
2. Deploy this Wave B classifier and operator safeguards.
3. Run dry-run, apply, and validation in that order.
4. Do not begin Wave C cause-specific recovery or make `disable_causes` mandatory until Wave B validation succeeds and its contract has been reviewed.
