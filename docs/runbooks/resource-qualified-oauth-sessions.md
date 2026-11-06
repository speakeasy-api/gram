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
  Consent for another resource inserts another session without changing the
  first. Consent provenance and cross-organization retirement rules are unchanged.
- Refresh and enrichment reload the exact source row. Refresh token persistence
  compares its row ID, subject, registration and timestamp, and never advances
  the consent generation. Revocation cannot be undone by an in-flight refresh.
- Existing agent attachments retain their exact source-row assignment. A new
  resource grant does not replace an attachment or confer access to that resource.
  Shared registration revocation/rotation retains its existing scope.
- Target-aware readers select the exact resource before considering a legacy
  unbound session, retaining established trailing-slash compatibility. Multiple
  matching bound grants fail closed. Registration-only readers refuse ambiguous multi-resource
  state. Existing downstream audience/resource checks remain authoritative;
  unbound tokens do not bypass those checks.
- Readiness, manual refresh and outage readers have `ForResource` methods.
  Registration-wide status intentionally omits a client with multiple grants;
  target-specific surfaces must use the resource-qualified method.

## Required deployment order

This is an application-first, schema-second rollout. It is not safe to deploy
the index migration while old writers still use
`ON CONFLICT (subject_urn, remote_session_client_id)`.

1. Deploy the compatibility application while retaining the old unique indexes.
   It uses targetless conflict detection and updates only the exact resource.
   Until the index migration, a second resource connection is temporarily refused
   without changing the first resource's usable grant. Same-resource reconsent,
   refresh and revocation continue to work.
2. Confirm every old application and worker writer is gone. Confirm target-aware
   consent/status consumers use the resource-qualified interfaces. This is an
   explicit operational gate, not something migration tooling can infer.
3. Apply the separately reviewed index migrations in order: first create the
   resource-qualified `NULLS NOT DISTINCT` indexes while retaining the old
   indexes, then remove the old indexes only after the new indexes are valid.
   This avoids an interval with no grant uniqueness enforcement. NULL legacy
   resources remain unique. Both resources can then retain independent grants.
4. Validate authorization, original/fresh/refreshed callers, resource-bound refresh,
   and revocation against both resources. Never weaken audience checks or replace
   this final simultaneous-resource contract with permanent early refusal.

Do not roll back to an old writer after the index migration: its conflict target
no longer exists, and its single-grant reader contract is no longer valid. A
rollback requires an explicitly reviewed data/model plan that preserves active
resource grants. No application-database migration is authorized by this document.
