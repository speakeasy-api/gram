# Agent management grants backfill

`agent-management-grants` is a PostgreSQL-only operator migration for the built-in organization administrator role. It adds these unrestricted grants to every recognized active administrator role:

- `agent:read`
- `agent:write`
- `agent:authorize`
- `agent:transfer`

The migration targets every active role with the reserved `admin` slug that can authorize users in an organization, including both global and organization-scoped administrator principals when both remain active. It never targets any other role slug or principal.

## Safety model

- The default mode is a non-writing dry run.
- Apply writes in keyset-ordered organization batches. Each batch commits independently. Inserts use the existing grant uniqueness key and normalize only a conflicting canonical grant's legacy effect, so restarting from the beginning after interruption is safe.
- Apply adds canonical grants only. It does not remove unexpected rows, replace existing role policy, change custom roles, or change unrelated grants.
- Every successful apply ends with complete-population verification at a repeatable-read snapshot. After an interrupted or failed apply, rerun apply and then run standalone verification before opening the rollout gate.
- Verification reports aggregate counts and at most `-sample-limit` internal organization-ID/principal-URN/scope records per defect category. It never reads or reports organization names, user IDs, emails, role names, or selector values.
- Verification fails unless every organization has at least one active administrator role, every active administrator principal has all four canonical unrestricted allow grants, and no other `agent:*` rows exist on those roles. Noncanonical selectors or effects on one of the four scopes are unexpected rows.
- `-apply` requires `GRAM_ENVIRONMENT` and `-confirm-environment` to exactly match `-environment`. Production apply also requires `-confirm-production=production`. Database credentials come only from `GRAM_DATABASE_URL`.

## Rollout gate

Agent-management endpoints fail closed behind the `gram-agent-management-m1` feature flag. Keep that flag disabled for a target environment or cohort until the focused M1 agent CI gate passes for the published `GRAM_CODE_SHA`, `-verify` exits zero against that complete target database, and the output has `summary.verification.ready_for_enforcement=true`. A dry run is evidence for planning only; it does not open the gate. Apply also blocks if its final verification finds unexpected rows or unresolved administrator roles.

If verification is blocked, investigate the bounded samples internally, repair the role data through the existing role/grant machinery, rerun apply if required grants are missing, and repeat verification. Do not add an `org:admin` runtime fallback or enable a partially verified cohort.

Set `GRAM_DATABASE_URL` to the target PostgreSQL URL with `sslmode=require`, `sslmode=verify-ca`, or `sslmode=verify-full`; plaintext and fallback-to-plaintext connections are rejected. Keep credentials in this environment variable, not in command flags. Set `GRAM_ENVIRONMENT` to the actual target environment. Set `GRAM_CODE_SHA` to the published commit being run so retained aggregate output identifies the implementation revision.

## Procedure

1. Dry-run in staging and retain the bounded aggregate output:

   ```sh
   go run ./server/cmd/tools/migrations agent-management-grants \
     -environment=staging
   ```

2. Apply in staging:

   ```sh
   go run ./server/cmd/tools/migrations agent-management-grants \
     -apply -environment=staging -confirm-environment=staging
   ```

3. Run database verification separately. Do not enable the `gram-agent-management-m1` feature flag unless the focused M1 agent CI gate passed for `GRAM_CODE_SHA` and this command exits zero and reports readiness:

   ```sh
   go run ./server/cmd/tools/migrations agent-management-grants \
     -verify -environment=staging
   ```

4. Repeat dry-run, apply, and verify in production. Production apply requires both confirmations:

   ```sh
   go run ./server/cmd/tools/migrations agent-management-grants \
     -apply -environment=production -confirm-environment=production \
     -confirm-production=production

   go run ./server/cmd/tools/migrations agent-management-grants \
     -verify -environment=production
   ```

An interrupted run can be restarted with the same apply command. Previously committed batches are no-ops, and missing later batches are filled. Keep `-batch-size` between 1 and 1000 and `-sample-limit` between 1 and 100.
