# Shared JWT key-algorithm binding follow-up

Status: open design/compatibility follow-up from PR #6571,
thread `PRRT_kwDOPZim8M6j5hBG` (discussion 4050791228).
This file tracks the work locally; no external issue has been created.

The resolver enforces a declared JWK `alg`, rejects symmetric algorithms, and
matches omitted-`alg` keys by key type and curve. An RSA key with no `alg` can
therefore verify more than one RS*/PS* algorithm. The global allowlist and issuer
metadata intersection do not establish RFC 8725 section 3.1's one-algorithm-per-key
binding. This is inherited behavior, not evidence of signature forgery.

Before changing shared verification, choose an explicit provider/local policy
for keys without `alg`. Do not infer a durable binding from the first untrusted
JWT header. Define how the binding survives cache eviction, key rotation, and
shared key material across registrations. Assess compatibility for existing
providers that omit optional JWK `alg`; requiring it without a migration plan
would reject previously supported providers.

Completion criteria:

- Persist or deterministically derive one allowed algorithm per signing key from
  trusted configuration; reject conflicting declarations at verification.
- Exercise a single RSA public key with valid signatures under RS256 and PS256
  (and another RS* hash). Accept only the configured algorithm, with and without
  JWK `alg`; prove cache refresh/rotation does not reset the binding.
- Preserve existing rejection of `none`, HMAC, incompatible key types/curves,
  and mismatched explicit JWK `alg`.
- Document rollout and test all consumers (ID tokens, access tokens, and ID-JAG).
