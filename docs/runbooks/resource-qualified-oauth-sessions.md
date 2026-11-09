# Resource-qualified upstream OAuth sessions

One human can use one OAuth registration for multiple upstream resources. The
upstream token state is identified by subject, registration and the recorded
OAuth resource. This does not add server-specific `mcp:auth` grants, copy
registrations, or change the existing credential resolver or owner checks.
Servers that deliberately use the same registration and resource share that
resource's current tokens and consent generation.

## Lifecycle

- Consent writes hold the existing registration lock. Same-resource reconsent
  advances only that grant's generation and replaces its tokens and identity.
  After index cutover, within the existing consent tenant, another resource inserts
  another session without changing the first. Cross-organization consent retains
  the existing retirement rule: all of that subject/registration's grants from
  another organization are tombstoned before the new tenant's grant is written.
- Refresh and enrichment reload the exact source row. Refresh token persistence
  compares its row ID, subject, registration and timestamp, and never advances
  the consent generation. Revocation cannot be undone by an in-flight refresh.
- Existing agent attachments retain their exact source-row assignment. A new
  resource grant does not replace an attachment or confer access to that resource.
  Shared registration revocation/rotation retains its existing scope.
- Target-aware readers select the exact resource before considering a legacy
  unbound session, retaining established trailing-slash compatibility. Multiple
  matching bound grants fail closed. Reconsent for a trailing-slash-equivalent
  audience retains its source row while saving the new provider-facing spelling. Registration-only readers refuse ambiguous multi-resource
  state. Existing downstream audience/resource checks remain authoritative;
  unbound tokens do not bypass those checks.
- Readiness, manual refresh and outage readers have `ForResource` methods.
  Registration-wide status intentionally omits a client with multiple grants;
  target-specific surfaces must use the resource-qualified method. Focused
  disconnect and auto-refresh preference mutations must use `ForResource` too:
  they affect only a matching bound audience, never an inferred legacy NULL
  resource. Unqualified multi-grant mutations return a failed precondition.
  Focused mutations reject an empty resource instead of selecting a singleton
  from another audience. Focused refresh and verification also require a bound
  matching resource; they never mutate a legacy unbound source by inference.
  To verify a focused connection, callers must pass the qualified status's row
  ID and CAS timestamp to `EnrichRemoteSessionForResource` and
  `RecordRemoteSessionValidationForResource`. Consent/action callers, typed
  lifecycle methods, direct and gateway readiness, and Platform MCP member
  status ship together in this compatibility application. Unqualified legacy
  singleton operations retain their existing contract.
- Legacy unbound grants remain individually revocable and configurable through
  `DisconnectRemoteSessionByRef` and `SetRemoteSessionAutoRefreshByRef`. These
  require an explicitly selected source reference, with subject, client, tenant,
  issuer-binding and timestamp checks. They are not a fallback for guessing a
  focused server's legacy source.

## Required deployment order

Use three separately reviewed PRs and release checkpoints: expand indexes,
application compatibility, then contract indexes. The expand-only migration
retains old uniqueness and supports old binaries. It is not safe to deploy the
contract migration while old writers still use
`ON CONFLICT (subject_urn, remote_session_client_id)`.

Keep this grant-cardinality work deferred until the enforcement rollout is
proven. Both enforced and compatibility paths support this application;
the enforcement flag is not an off-switch for expanded grant cardinality.
Removing the old indexes enables multiple resource grants even with enforcement
disabled. Keep independent-RSC setup until the complete rollout is deployed and
validated.

1. Deploy the expand-only migration (#8738). Create the resource-qualified
   `NULLS NOT DISTINCT` indexes over the routing identity (`rtrim(resource, '/')`),
   with `WHERE deleted IS FALSE`, while retaining both old unique indexes.
   Verify both replacement indexes are valid. Old uniqueness still prevents
   multiple resource grants.
2. Deploy the compatibility application (#8705) while retaining the old indexes.
   It uses targetless conflict detection and updates an exact resource grant or
   safely adopts a singleton NULL/bound transition. Until contract, a second
   resource connection is temporarily refused without changing the first
   resource's usable grant. Same-resource reconsent, refresh and revocation
   continue to work.
3. Verify the deployed compatibility revision and confirm every old application
   and worker writer is gone. This is an explicit operational gate, not something
   migration tooling can infer. Deploy and verify all required resource-qualified
   consent, readiness/actions, typed lifecycle and Platform MCP status consumers.
   Prove that composition works safely on both index shapes before contract.
4. Promote the separate contract migration PR only after those checks pass.
   Confirm both replacement indexes are valid, then remove the old indexes.
   This preserves uniqueness throughout the transition; NULL legacy resources
   remain unique. Verify actual migration completion and application health.
   Keep contract in draft until its application and consumer prerequisites are
   deployed and validated; do not bundle it with the compatibility release.
5. Validate authorization, original/fresh/refreshed callers, resource-bound refresh,
   and revocation against both resources. Never weaken audience checks or replace
   this final simultaneous-resource contract with permanent early refusal.

Do not roll back to an old writer after contract: its conflict target
no longer exists, and its single-grant reader contract is no longer valid. A
rollback requires an explicitly reviewed data/model plan that preserves active
resource grants. No application-database migration is authorized by this document.

## Failed concurrent index build

A cancelled or timed-out `CREATE UNIQUE INDEX CONCURRENTLY` can leave an invalid
index. Atlas can record a partially applied version, such as 0/2 statements.
A retry then fails because the invalid index still owns its name. Do not add
`IF NOT EXISTS`: that would retain the invalid index instead of repairing it.

A human operator must select and verify the target environment before repair.
Keep the compatible application running, and confirm that both old uniqueness
indexes still exist. If either old index is missing, stop and investigate the
rollout state before continuing. Do not retire the old indexes after a failed
replacement build.

Inspect both replacement indexes:

```sql
SELECT n.nspname AS schema_name, c.relname AS index_name,
       i.indisvalid, i.indisready
FROM pg_index AS i
JOIN pg_class AS c ON c.oid = i.indexrelid
JOIN pg_namespace AS n ON n.oid = c.relnamespace
WHERE n.nspname = 'public'
  AND c.relname IN (
    'remote_sessions_subject_client_resource_key',
    'remote_sessions_subject_client_issuer_resource_key'
  );
```

For each replacement index that exists with `indisvalid = false`, run its
corresponding command below. Run only the commands for invalid indexes, outside
any transaction. Leave valid replacement indexes and both old indexes intact.

```sql
DROP INDEX CONCURRENTLY public.remote_sessions_subject_client_resource_key;
DROP INDEX CONCURRENTLY public.remote_sessions_subject_client_issuer_resource_key;
```

Rerun the original Atlas apply through the normal authorized deployment process.
Do not bypass the failed version or edit Atlas revision records. Verify that both
replacement indexes exist with `indisvalid = true` before proceeding to the
migration that retires the old indexes. This procedure does not authorize access
to, or changes in, any environment.
