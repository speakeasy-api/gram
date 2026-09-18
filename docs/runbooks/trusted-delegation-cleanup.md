# Trusted OIDC delegation credential cleanup

AIM-69 retains upstream OIDC assertions and optional refresh credentials encrypted at rest. Expired assertions and unusable secrets on inactive, orphaned, or deleted rows must be erased within the proposed 24-hour retention ceiling. Explicit revocation clears credentials in the write path; this shared sweep covers inactive rows. Cleanup never refreshes tokens or starts work per human, login, or tool call.

## Schedule and limits

The worker registers `TrustedDelegationCleanupWorkflow` through a queue-scoped schedule named `v1:trusted-delegation-cleanup:<TASK_QUEUE>`. It runs hourly with overlap skipped. Existing schedules are not updated by worker startup. There is no continue-as-new loop.

Each run uses one `CleanupTrustedDelegationCredentials` activity. It performs at most 100 sequential SQL batches of 500 rows (50,000 rows per attempt), stopping at the first partial batch. Each batch is a separate bounded database operation. Retries are idempotent because SQL only selects secrets still eligible for erasure. Activity execution is limited to 10 minutes and three attempts; the workflow has a 35-minute run timeout.

Temporal actions/month per namespace ≈ 720 workflow starts + 720 activities = **1,440 normally**, or **2,880 with all three activity attempts**. Timers, signals, and child starts: zero. Cost scales with fixed schedule copies, not organizations, humans, messages, or tool calls. Each preview task queue adds its own fixed schedule in the dev namespace.

## Retention operations

The hourly cadence targets erasure well inside 24 hours under normal operation; a schedule alone cannot guarantee retention during an outage or an excessive backlog. Monitor failed cleanup runs, schedule-registration errors, and the explicit `trusted delegation credential cleanup batch budget exhausted` failure. Investigate failures before the oldest eligible secret reaches 24 hours. Restore worker/database availability or increase the reviewed batch capacity when sustained backlog exceeds the hourly budget. Do not diagnose by logging ciphertext, tokens, human identifiers, claims, or provider responses.

The organization-admin delegation status API is independent of this schedule. It reads only sanitized current-configuration observations within 30 days; it never discovers metadata, decrypts credentials, or attempts a refresh. Credential presence is not proof of future refresh success.

No demo credential fixture is added: this is a security/storage API change with no new dashboard surface, and real or synthetic retained credentials are not needed to populate the demo organization.

## Delegation runtime and recovery

The portable path is OIDC only. AIM-70 supplies a validated, provisioned-human handoff after organization access checks. No SAML assertion capture or SAML bootstrap is implemented. Okta requires a customer-configured OIDC app for this path; an OIN SAML requesting app alone is not sufficient. No production provider or XAA compatibility claim follows from the mock-provider tests.

`DelegationService.Resolve` is an internal integration point for AIM-62. Its caller must supply a `DelegationAuthorizer` that proves current issuer/client binding and current human-delegation authority. Raw client IDs confer no authority; retaining credentials does not authorize an autonomous agent. Management APIs never return these credentials. An assertion must remain valid beyond a 60-second safety window; renewal happens only on demand.

Refresh ownership is a durable database claim, not an expiring lease. Claim acquisition and completion advance a generation. Callbacks and completions use generation compare-and-swap; losing responses cannot overwrite or erase a winner. A timeout, intermediary HTTP 5xx, lost response, post-refresh verification dependency failure, or failure to persist rotation leaves the generation claimed. It is deliberately **not** unlocked by a timer or another process. Reauthenticate or explicitly revoke it; never clear a claim manually to retry a possibly spent refresh token. Definitive retryable failures, such as HTTP 429, use a one-minute backoff. A JWKS outage after a successful token response is not an identity mismatch: it exposes no new assertion and quarantines the submitted generation instead of erasing credentials or replaying a possibly spent token. Successful rotation without an ID token is retained, but the portable path requires reauthentication rather than repeated refresh requests.

Explicit refresh-response lifetime bounds apply even when no replacement refresh token is returned; a zero bound erases the expired credential, and omission preserves any previously known bound. Ordinary login preserves a usable existing refresh credential when the provider omits a new one. Its original nonce binding is preserved. A fresh callback does not revive an in-flight or ambiguous refresh credential. Actual offline-consent refusal is suppressed for the same human and relevant policy revision. Discovery timestamps and cosmetic edits do not reset suppression; client-secret rotation and active signing-key rotation do. Static registration hashes support network-free admin status reads; offline-request hashes additionally include relevant advertised provider capabilities.

Deploy the additive `trusted_issuer_sessions` migration before enabling the readers/writers. The runtime is wired through the shared MCP service constructor, including the full server, MCP-only serving tier and private ingress runtime. The dependent AIM-70 branch must land first, or this work must be reviewed as a stack based on it.

An explicit same-human retry reuses the CSRF-protected consent action endpoint (`POST /mcp/<MCP_SLUG>/connect/remote-session`, form action `retry_delegation`, with the current consent `state` and `csrf_token`). It consumes the previous state without extending its TTL, repeats minimal OIDC login bound to the canonical human, and permits one optional offline-consent step. It never trusts posted human/client identifiers. This is not a separate dashboard connection feature.
