# Agent credential expiry and unattended operation

DNO-1421. Research proposal; no production behavior changes.

## Current behavior

The server defaults agent keys to 90 days and rejects lifetimes over 365 days
(`server/internal/keys/agent_keys.go`, `parseAgentKeyExpiry`). Database queries
reject expired credentials (`server/internal/keys/queries.sql`). Extending a
running process's session cannot be treated as extending its credential.

`RotateKey` requires an ordinary human caller. It locks the existing key, revokes
it, creates its replacement, and commits both changes in one transaction. The
new plaintext key is returned once. This is replacement, not a two-key overlap:
an interrupted response can leave the caller without the new secret and the old
key already revoked. An agent cannot safely automate this endpoint merely by
presenting its existing agent key.

Runtime admission also checks live principal eligibility, ownership, and grants
(`server/internal/agents/runtimepolicy/credential_admission.go`). Any renewal
design must retain these checks rather than snapshotting authorization forever.

## Recommendation

Ship expiry visibility and reminders first. Treat device-mediated renewal as a
separate protocol, dependent on DNO-1341, and reserve workload identity for
environments with a verifiable external identity provider. Do not solve unattended
operation by introducing non-expiring bearer keys.

| Approach                   | Benefit                                                              | Remaining failure mode                                                                           |
| -------------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------ |
| Owner reminders            | Smallest operational change; existing human issuance remains intact  | Owner can miss reminders; runtime still expires                                                  |
| Device-mediated renewal    | Can maintain installed agents without repeated manual secret copying | Device authentication, delivery acknowledgement, replay protection, and recovery need a protocol |
| Workload identity exchange | Removes distributed long-lived secrets for supported runtimes        | Requires trusted issuer, audience and subject binding; does not cover every desktop installation |

## First implementation slice: visibility and reminders

1. Show the expiry date and a consistent near-expiry state wherever an agent key
   is managed. Never display or log the secret to explain expiry.
2. Select active, expiring agent credentials from authoritative database state.
   Start with proposed 30-, 7-, and 1-day reminders; make these thresholds a
   product decision before implementation.
3. Resolve the current eligible owner at delivery time. Skip revoked or replaced
   keys, suppress duplicates with a key/deadline/threshold identity, and avoid
   contacting an old owner after reassignment. Recheck before sending.
4. Link to authenticated key management. A reminder must contain neither a key
   nor a credential-bearing installer link. Use the existing approved delivery
   channel; this proposal does not provision a notification service.
5. Expose delivery failures operationally without treating a reminder as proof of
   rotation. Expiry enforcement remains unchanged.

Implementation must use the repository's background-work and transactional-email
workflows. No schedule or email is introduced by this research PR.

## Renewal protocol requirements

Do not relax the human-only rotation endpoint. Introduce a narrowly scoped device
renewal capability only after device enrollment and authorization are defined.
Bind it to an agent, enrolled device, and current owner authorization; it must not
grant general key issuance or allow scope amplification.

A proposed handshake issues a successor while the predecessor remains usable for
a short bounded overlap, stores the successor securely on the device, verifies a
request using it, and then revokes the predecessor. Retry must be idempotent and
must not mint unlimited keys. Decide how a lost issuance response is recovered
without storing recoverable plaintext credentials on the server. Explicit owner
revocation or device unenrollment must invalidate both credentials immediately.

An expired, revoked, or ineligible credential must never bootstrap its own
renewal. An offline device that misses the renewal window needs an explicit human
re-enrollment path. Device enrollment credentials need their own expiry and
revocation policy; moving the same long-lived secret elsewhere is not a solution.

## Acceptance evidence before implementation is considered complete

- Clock-boundary tests for expiry and each reminder threshold, including retries.
- Revocation, ownership change, disabled owner, and replacement between selection
  and delivery produce no stale reminder or expanded authority.
- A lost rotation response has a documented, exercised recovery path.
- Concurrent renewals, device crashes, offline devices, and replayed acknowledgments
  cannot leave unlimited valid successors or bypass expiry.
- Current agent, owner, and credential permission restrictions remain effective
  after renewal, including scope reductions during the handshake.

Open decisions: notification channel and thresholds; maximum overlap; ownership
transfer semantics; device enrollment guarantees from DNO-1341; supported workload
identity issuers. These are prerequisites for an automatic renewal implementation,
not reasons to delay making expiry visible.
