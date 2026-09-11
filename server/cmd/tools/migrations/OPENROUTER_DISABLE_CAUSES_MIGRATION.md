# OpenRouter disable-cause classifier

`openrouter-disable-causes` is a PostgreSQL-only operator migration. It classifies legacy `openrouter_api_keys` rows whose `disable_causes` is `NULL`; it never calls OpenRouter. Run it only from the published revision approved for the rollout.

## Safety model

- The default mode is dry-run. There is no `-dry-run` flag.
- `-apply` and `-manual-override` are the only writing modes. Every write requires `-confirm-environment` to exactly match `-environment`. Production writes additionally require `-confirm-production=production`. Manual overrides also require the explicit `-confirm-manual-override` acknowledgement.
- Automatic billing classification fails closed. `billing_inactive` requires a current `free` account with no subscription and a durable `organization:payg_deactivated` audit whose organization subject and snapshots prove a `payg` to `free` transition. Never-PAYG, malformed, and contradictory rows remain ambiguous.
- Admin evidence requires the production audit identity: subject ID `<ORG_ID>/<KEY_TYPE>` and subject type `openrouter_api_key`.
- Output is aggregate JSON only. Blocked logs expose only `ambiguous_rows`, `validation_failed`, `override_conflict`, `database_or_timeout`, or `unexpected`; they do not include row identifiers, override contents, credentials, or database URLs.
- Application code never infers a cause while handling a business event. In particular, trial demotion remains transactional and fail-closed for `NULL`: the classifier is the single owner of historical provenance decisions. Temporal marks that data-contract error non-retryable within the current run, but the hourly sweep continues reporting the undemoted trial until classification succeeds.

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

5. If an authorized investigation establishes evidence unavailable to the classifier, provide one protected override JSON object on standard input. Set `GRAM_OPENROUTER_DISABLE_CAUSES_OVERRIDE_TOKEN` out of band and use all write confirmations. Never paste identifiers, tokens, or manifests into tickets, commits, or logs. Store the object in a protected file and run the complete production command:

   ```sh
   cat "$PROTECTED_OVERRIDE_FILE" | go run ./server/cmd/tools/migrations openrouter-disable-causes \
     -manual-override -environment=production -confirm-environment=production \
     -confirm-production=production -confirm-manual-override
   ```

## Retries and recovery

Lock and statement timeouts are bounded by `-lock-timeout`, `-statement-timeout`, and `-max-lock-retries`. A blocked run is safe to retry after correcting the reported aggregate category: writes are batched, compare-and-set, resumable, and idempotent. Do not increase timeouts until database contention or query failure has been identified.

If the category is `database_or_timeout`, verify connectivity and database health without printing `GRAM_DATABASE_URL`. For `ambiguous_rows` or `validation_failed`, inspect aggregate counts and use privacy-preserving database queries; do not export row-level data.

## Rollout order

1. Deploy the nullable column and its `{}` default. The default affects only future inserts and is safe while ambiguous `NULL` rows remain. Do not add `NOT NULL` or rewrite existing rows.
2. Deploy application writers that explicitly create `{}` and mutate named causes. This includes routing the legacy generic disable entry point through `admin_lock`; it must fail closed rather than overwrite a `NULL` row. Complete this step before running the classifier so application traffic cannot reintroduce `NULL`.
3. Deploy this classifier and operator safeguards, then run dry-run, apply, and validation in that order. Trial demotion may continue surfacing non-retryable data-contract failures during this interval; those failures preserve the transaction and are not permission to skip a row.
4. Treat any validation failure or remaining ambiguity as a rollout stop. A future `NOT NULL` migration is allowed only after production validation reports zero live `NULL` rows and `skipped_deleted` is zero, every writer in every deployed version is known to preserve classified state, and that precondition has been reviewed explicitly.
