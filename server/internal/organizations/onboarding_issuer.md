# Onboarding sign-in preparation

`GET /rpc/organizations.listSetupTasks` is a personalized onboarding entry point,
not a strictly read-only inspection API. When its result contains the visible
Anthropic admin-controls step, it prepares a missing organization-level
user-session issuer before returning the checklist. This also covers existing
organizations and direct links that bypass the onboarding survey. Responses use
`Cache-Control: private, no-store`. Clients must finish loading the checklist
before fetching gateway availability, or refetch availability afterward.

Preparation requires an organization administrator's user session. Member reads,
API-key sessions, support sessions, impersonated sessions, disabled organizations,
and hidden-card previews do not provision. A transaction locks the organization,
checks for live organization-level issuers, and creates and audits an issuer only
when none exists. Manual organization-issuer creation and issuer scope moves
take the same lock before issuer or remote-provider locks.
Explicit manual creation may still create multiple issuers; preparation never
chooses among them, changes them, or migrates project issuers.

The defaults match server-created user-session issuers: interactive sign-in,
fourteen-day sessions, and the create query's explicit OAuth client-admission
policy. Open client admission does not grant anonymous access: interactive login
verifies membership in the endpoint's organization before issuing a token.
Gateway opt-in and server access grants remain unchanged. Gateway availability
must still come from the existing authoritative gateway endpoint.

There is no `inspect_setup` tool in this server's Platform MCP catalog, and no
Platform MCP tool calls `ListSetupTasks`. The existing
`get_my_organization_gateway` tool remains read-only and does not provision.
Any future MCP wrapper around the checklist must not advertise read-only
semantics unless it uses a separate read-only projection path.

The onboarding survey and staff playbook assignment alone are insufficient
preparation hooks: an existing organization may already have an assigned
playbook and enter the connection step directly. Lazy preparation is deliberately
limited to this onboarding entry point, not general organization reads, gateway
discovery, or sign-in.
