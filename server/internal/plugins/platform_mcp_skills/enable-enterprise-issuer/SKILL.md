---
name: enable-enterprise-issuer
description: Guide an organization administrator through Speakeasy AI Control Plane enterprise issuer setup, retrieve the public issuer URI, and distinguish configured trust from verified MCP access.
---

# Set up enterprise authorization

Use the executing client's authenticated Speakeasy AI Control Plane (AICP)
Platform MCP connection. This workflow retrieves one organization's issuer and
guides its administrator through dashboard setup. The available issuer tool is
read-only; it cannot enable, disable, rotate, or replace an issuer.

## Establish the organization

1. Successfully call `list_projects` through this same connection to establish
   authenticated discovery. Package installation or another client's login is
   not authority. This issuer is organization-scoped; do not choose a project
   or pass a project ID to its getter.
2. Call `get_internal_session_issuer` with `{}`. Use its exact `organization_id`,
   `state`, optional `issuer`, and `next_step`. Match any organization the user
   specified; otherwise ask the user to confirm the returned organization.
   Never guess an organization name or construct an issuer URI.
3. Live membership, external organization-admin access, SSO entitlement and
   deployment availability remain authoritative. For `unavailable` or
   `entitlement_required`, explain the returned limitation and stop. Do not
   change entitlements, sign-in providers, or deployment settings to bypass it.

## Guide setup and retrieve the issuer

Each user must sign in to Speakeasy through the organization's enterprise SSO
before using enterprise authorization. Google or another social sign-in alone
is insufficient. SSO entitlement and an enabled issuer do not prove that the
current user has an enterprise SSO session. Explain this prerequisite during
setup and when returning an already-enabled issuer. A configured vendor grant
is not required merely to enable the issuer; vendor trust is a separate step.

For `setup_required` or `disabled`, explain that enabling provisions or resumes
the organization's public issuer; it does not establish vendor trust or grant
user access. Ask the organization administrator to open **IDP and SSO → Single
sign-on → Enterprise authorization** and choose **Enable enterprise
authorization** for the confirmed organization. There is no issuer mutation
tool in this workflow. Do not substitute raw API requests, database writes,
credentials, or an invented tool.

After the administrator completes setup, call `get_internal_session_issuer`
again. Require the same organization, `state: enabled`, and a nonempty `issuer`
before returning the URI. A transport failure is an uncertain read, not proof
that setup failed; read again before suggesting another dashboard action.
For `signing_paused`, explain signing is unavailable and ask the organization
administrator to contact Speakeasy support. For `keyless`, explain that no active
signing key exists and ask an organization administrator to contact Speakeasy
support for the signing-key recovery procedure. Recovery is not available on
this dashboard page or through this read-only tool. Do not attempt key rotation
or revocation, or imply that support staff may mutate the organization's issuer.

## Connect a supported service

Show the exact public issuer URI from the successful read. Explain that the
service's administrator must register it in that service's enterprise-managed
authorization settings. This is a separate trust change requiring the user's
explicit authorization; do not change ordinary employee SSO login settings or
replace an existing trusted issuer automatically.

If the user wants to configure an MCP connection too, use `list_projects`,
`find_mcp`, and `get_mcp` to identify an exact project and MCP server. Follow its
supported setup handoff and obtain separate authorization for any provider or
binding changes. Then call `get_mcp_readiness` for current readiness evidence.
An enabled issuer or a saved vendor setting does not prove live user access.
Only a separately authorized test using the connected MCP can establish that;
never impersonate another user or create test data without permission.

Report the confirmed organization, public issuer URI, and the evidence actually
observed. Distinguish issuer enabled, vendor trust configured, readiness checked,
and a live MCP operation verified. Never request or expose API keys, passwords,
tokens, OAuth codes, client secrets, or secret headers.
