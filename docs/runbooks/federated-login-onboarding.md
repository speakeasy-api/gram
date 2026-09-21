# Federated MCP login: identity trust contract

Federated MCP login trusts the configured enterprise OpenID Provider (OP) to
assert identity within one Gram organization. Before configuring a trusted
issuer/client link, administrators must validate the following contract.

## Identity mapping and email ownership

Gram maps the verified ID token's email claim to an active directory entry in
the organization. It does **not** persist an `(iss, sub)` account binding. A
directory entry's stored Gram `user_id` is authoritative, even if the account's
current email differs. Only entries without that link use case-insensitive email
matching to an existing active Gram account with active organization membership.
Missing or ambiguous mappings are rejected; login does not provision an account.

- The trusted enterprise issuer must control email ownership. End users must not
  be able to assert another provisioned person's email address.
- Directory synchronization must handle email renames, address reuse, and
  reassignment safely. Remove or correct stale `user_id` links before a
  reassigned address can authenticate. Do not assume that changing an account's
  email automatically repairs existing directory links.
- Verify active directory records and organization membership when onboarding,
  renaming, reassigning, or deprovisioning an identity. Suspend federation for the
  affected identities until inconsistent mappings are corrected.
- If these guarantees cannot be made, do not enable this email-based federation
  mapping. Use a separately designed stable identity binding instead.

Gram rejects explicit `email_verified: false`, but accepts an absent claim under
this enterprise provisioning contract. Even `email_verified: true` does not make
an email address a permanent unique identifier. OIDC defines stable identity by
issuer plus subject, not email; see [OIDC Core §5.7](https://openid.net/specs/openid-connect-core-1_0.html#ClaimStability)
and [§5.1](https://openid.net/specs/openid-connect-core-1_0.html#StandardClaims).

## Provider validation requirements

The provider must advertise authorization-response issuer support and return an
exact `iss` in authorization responses. Providers without this support are
rejected before login begins. An ID token must target only the configured Gram
client; additional, untrusted audiences are rejected even when `azp` matches.
RSA ID-token signing keys must be at least 2048 bits.

Shared signing-key algorithm binding for JWKs without `alg` remains an explicit
[open review discussion](https://github.com/speakeasy-api/gram/pull/6571#discussion_r4050791228).
This document does not claim that the follow-up is implemented.
