---
name: enable-enterprise-issuer
description: Guide an organization administrator through Speakeasy AI Control Plane enterprise issuer setup, retrieve the public issuer URI, and distinguish configured trust from verified MCP access.
---

# Set up enterprise-managed authorization

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
   `state`, `can_enable`, optional `issuer`, and `next_step`. Match any organization the user
   specified; otherwise ask the user to confirm the returned organization.
   Never guess an organization name or construct an issuer URI.
3. Live membership, external organization-admin access, verified domains and
   deployment availability remain authoritative. For `unavailable`, explain the
   returned limitation and stop. For `verified_domain_required`, explain that
   the organization needs a verified company domain first, and ask an
   organization administrator to verify one in the dashboard under
   **Organization** → **IDP and SSO** → **Single sign-on**, then read the issuer
   again. This workflow has no domain tool: do not verify domains yourself or
   change sign-in providers or deployment settings to bypass it. The same
   handoff applies when `get_xaa_readiness` reports `verified_domain_required`;
   after the domain is verified, call `get_xaa_readiness` again.

## Guide setup and retrieve the issuer

Each user needs a company sign-in to Speakeasy (SSO, company-managed Google or
Microsoft account, magic link, or password) with an email on one of the
organization's verified domains. Personal Google or Microsoft accounts don't
qualify. An enabled issuer does not prove that the
current user has a qualifying sign-in. Explain this prerequisite during setup and
when returning an already-enabled issuer. A configured vendor grant
is not required merely to enable the issuer; vendor trust is a separate step.

For `setup_required` or `disabled`, first require `can_enable: true`. If it is
false or missing, report `next_step` and stop; do not direct the administrator
to enable an unavailable issuer. When eligible, explain that enabling provisions or resumes
the organization's public issuer; it does not establish vendor trust or grant
user access. Ask the organization administrator to open **IDP and SSO → Identity
providers → Speakeasy** and, under **Status** for the confirmed organization,
choose the button whose label begins **Enable Enterprise Managed**. There is no issuer mutation
tool in this workflow. Do not substitute raw API requests, database writes,
credentials, or an invented tool.

After the administrator completes setup, call `get_internal_session_issuer`
again. Require the same organization, `state: enabled`, and a nonempty `issuer`
before returning the URI. A transport failure is an uncertain read, not proof
that setup failed; read again before suggesting another dashboard action.
For `signing_paused`, explain signing is unavailable and ask the organization
administrator to contact Speakeasy support. For `keyless`, explain that no active
signing key exists, so nothing is signed, and ask an organization administrator
to open **IDP and SSO → Identity providers → Speakeasy** and, under **Signing
keys**, choose **Finish setting up a new key**. The
issuer URI stays the same. If the administrator suspects a key is compromised,
point them to **Replace compromised key** on the same page. For a routine key
change, **Rotate key** there publishes a new key, which an hourly background
check activates once it is ready, usually within about an hour;
**Activate new key** switches to it sooner. There is no key
mutation tool in this workflow: do not attempt rotation or revocation yourself,
or imply that support staff may mutate the organization's issuer.

## Connect a supported service

Show the exact public issuer URI from the successful read. Explain that the
service's administrator must register it in that service's enterprise-managed
authorization settings: registering the URI alone changes nothing. Once the
organization has a Speakeasy issuer and sign-in enforcement is on, servers that
use Speakeasy's own sign-in (their sign-in trusts no external identity
provider) use it by default; without sign-in enforcement it cannot vouch.
Speakeasy vouches only for those servers: a server that signs people in through
an external identity provider such as Okta is vouched for only by that
provider, even when the service accepts Speakeasy. Changing a server's sign-in
is a separate trust change requiring
the user's explicit authorization; do not change ordinary employee SSO login
settings or replace an existing trusted issuer automatically.

If the user wants to configure an MCP connection too, use `list_projects`,
`find_mcp`, and `get_mcp` to identify an exact project and MCP server. Follow its
supported setup handoff and obtain separate authorization for any provider or
binding changes. This workflow cannot choose Speakeasy as an MCP server's
enterprise authorization provider. With nothing saved the service accepts every
identity provider, and who vouches is derived:
the server's trusted external identity provider alone, or the organization's Speakeasy issuer when the sign-in trusts none.
An organization admin chooses which identity providers a service accepts on the
dashboard's **Organization** → **IDP and SSO** → **Identity providers** →
**Services** page for the vendor, or through the organization's
`setEMAProviders` management API; pass `expected_providers` from the list you
last read, and on a conflict re-read and reconcile the list before retrying.
The choice is stored as an ordered list, but
each server uses only the provider its sign-in goes through, so the list acts
as the set of accepted providers. Saving a provider list needs the
provider-list rollout; restoring the default needs only organization admin. A
saved list that leaves out a server's provider leaves that server with nobody
to vouch; the dashboard flags such servers as needing attention. The MCP server's **Settings** page
turns enterprise-managed authorization on or off for that server and shows the
sign-in, who vouches, and what the vendor has to trust.
Then call `get_xaa_readiness` with the exact project and MCP server IDs for
enterprise authorization readiness. Do not substitute a Platform MCP registration
ID or generic registration readiness for this check.
An enabled issuer or a saved vendor setting does not prove live user access.
Only a separately authorized test using the connected MCP can establish that;
never impersonate another user or create test data without permission.

Report the confirmed organization, public issuer URI, and the evidence actually
observed. Distinguish issuer enabled, vendor trust configured, readiness checked,
and a live MCP operation verified. Never request or expose API keys, passwords,
tokens, OAuth codes, client secrets, or secret headers.
