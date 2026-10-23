# External workload inference: policy versions and rollout

## Issuance contracts

Reader support and issuance are separate contracts:

| Credential family                                                            | Issued policy version              |
| ---------------------------------------------------------------------------- | ---------------------------------- |
| Existing general delegated credentials and workload MCP exchanges            | 4 (`IssuedDelegatedPolicyVersion`) |
| Existing assistant MCP credentials and internal assistant inference ceilings | 5 (`AssistantIssuedPolicyVersion`) |
| New external `urn:gram:inference` workload credentials                       | 6 (`InferenceIssuedPolicyVersion`) |

Before this feature, the assistant issuers used `CurrentDelegatedPolicyVersion`, which was 5. They are now pinned to 5, not downgraded to 4. Internal assistant ceilings exclude scopes introduced after version 5, preserving existing grants and exclusions even when the agent also has an external inference grant.

Version 6 is necessary because delegated-policy validation interprets the policy version as its runtime scope registry version. `inference:execute` becomes safe only at registry version 6. Adding it to registry versions 4 or 5 would change the authority of old credential formats. No default role gains inference permission, and MCP/chat grants do not imply it. `CurrentDelegatedPolicyVersion = 6` means the reader understands version 6; it does not advance existing issuers.

## Mixed replicas and rollback

Existing MCP credentials continue to use version 4, and existing assistant credentials continue to use version 5. Both remain readable by pre-feature replicas that supported version 5. This does not promise compatibility with releases older than the pre-feature version-5 baseline.

External inference is an audience-specific opt-in, not backward-compatible availability. Old replicas do not implement the new exchange/completion audience and cannot authorize these credentials. Their policy reader also rejects version 6 before decoding grants. During a mixed deployment, requests routed to old replicas fail closed; requests routed to upgraded replicas can succeed after all live admission checks. Complete deployment before advertising reliable external inference, or route both exchange and completion to upgraded replicas. A full rollback makes external inference unavailable until the feature is redeployed; do not rewrite version-6 rows as version 4 or 5, widen audiences, or bypass admission to restore availability.

User-session listing and revocation operate on tenant-scoped session rows and metadata, not on decoded delegated authority. Unknown-version sessions remain manageable without granting their policy. Existing management visibility and permission boundaries still apply; this feature does not add cross-project or organization-session visibility. Expiry, revocation, assignments, workload admission, and agent/owner grants remain live checks on each inference completion.

## Regression coverage

- `TestInferencePolicyVersionCompatibility`: version-5 reader ceiling rejects version 6; versions 4 and 5 cannot encode or decode inference grants, and their MCP policies remain readable.
- `TestAssistantWorkloadClientCredentialsEndToEnd`: persisted assistant MCP policy remains version 5.
- `TestInferenceCeilingPreservesVersion5`: internal assistant ceilings retain version-5 behavior without importing external inference authority.
- `TestUserSessionManagement_UnknownDelegatedPolicyVersion`: management can list and revoke an unknown-version session without interpreting its policy.

The reader-ceiling test exercises the same pre-decode version boundary with a version-5 maximum; it is not a multi-binary rolling-deployment test.
