# Assistant workload identity provisioning

New assistants receive a dedicated project-scoped agent, an exclusive assistant
binding, and exact workload admissions/assignments for their durable root
triggers. API creation and managed-assistant creation use the same transaction
contract. Existing assistants remain legacy until an authorized explicit upgrade.
Creation retains existing name uniqueness and request semantics; it does not add
an idempotency header, request-key store, or request fingerprint.

Initial agent grants are the intersection of configured assistant capabilities
and the provisioning actor's policy. The assistant creator and consenting actor
are retained separately in provisioning provenance. Later access edits use the
existing policy surfaces.

## Trust and lifecycle

The deployment's `GRAM_AUTHZ_ISSUER_URL` is the single Gram signing issuer origin.
Provisioning uses ordinary tenant/project trust registrations with that URL and
its existing `/.well-known/jwks.json` endpoint. A registration is not another
signing issuer, has no special issuer kind, and cannot omit its key endpoint.
The demo seed uses explicitly inert example URLs for its display-only records.

The exact subject is `assistant-trigger:<original-trigger-UUID>`. It does not
change for deliveries, users, retries, or continuation wakes. Continuations must
retain the durable original root, rather than use their own wake ID.

Assistant/trigger bindings retain original tenant/resource identifiers and
monotonic generations. Current reference tuples, lifecycle, owner eligibility,
exact admission and assignment, and configured issuer/JWKS must still match.
Retargeting explicitly retires the previous trigger binding. Assignment changes,
owner transfers, trust-key changes, and withdrawals retire affected bindings and
sessions transactionally; changing them back cannot revive old authority.
Suspending an agent or pausing an assistant temporarily denies identity resolution
without permanently retiring the binding or changing its generations.

Only `NEVER_CONFIGURED` may enter a legacy fallback. Unavailable, inconsistent,
revoked, hard-deleted, and tombstoned mappings must never fall back to a creator.

## Required execution contract for AIM-410

This change does not mint internal execution credentials or switch existing turns
to agent execution. Runtime integration must:

- Capture both binding generations and the complete pinned tenant, project,
  assistant, agent, issuer-registration, original-trigger, and exact-subject tuple.
- Use signed organization/project claims and compare them with the requested
  tenant **and** the pinned mapping. Shared issuer URL/key equality is not proof
  of tenant identity.
- Use a dedicated internal audience and credential type, separate from private
  MCP `speakeasy-identity+jwt` assertions. Do not use `mcpauthz.Mint` for internal
  authentication. The external workload bearer grant rejects private MCP and
  `gram-assistant-execution+jwt` credential types.
- Revalidate current mapping and eligibility before execution and resumption;
  enforce the immutable captured policy ceiling and current authority together.
- Preserve original trigger provenance across retries/continuations. Explicit
  revocation/reassignment must invalidate current execution without converting
  the mapping to missing/legacy state.

The existing Platform MCP exposes identity inspection and an explicitly confirmed,
human-authorized legacy upgrade. It does not allow a managed assistant to grant
itself an identity or treat a configuration-state field as execution permission.
