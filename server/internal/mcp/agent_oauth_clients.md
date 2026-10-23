# Agent OAuth clients

An ordinary agent can register a renewable asymmetric OAuth client through the
existing resource registration endpoint. This does not require an assistant,
runtime, trigger, or workload issuer.

Send `POST /mcp/<SLUG>/register` with `Authorization: Bearer <AGENT_API_KEY>` and
`Content-Type: application/json`:

```json
{
  "client_name": "Native agent",
  "agent_urn": "agent:<AGENT_ID>",
  "grant_types": ["client_credentials"],
  "token_endpoint_auth_method": "private_key_jwt",
  "jwks": { "keys": ["<PUBLIC_JWK>"] }
}
```

Replace the public-key placeholder with a public JWK object. The agent URN must
exactly match the authenticated agent. Registration checks the key's delegated
ceiling, the live agent policy, the current owner's policy, and the original
authorizer's policy for the target MCP resource. Anonymous DCR cannot establish
this trusted profile. The registration response contains a client ID and public
metadata, never a client secret or private key.

At the advertised token endpoint, send `grant_type=client_credentials`,
`client_id`, `scope=mcp:connect`, the exact `resource` URL, and an RFC 7523 client
assertion using the registered key. Existing assertion audience, expiry, replay,
and key-source validation apply. Tokens have a resource-bound audience and a
maximum lifetime of 15 minutes. There is no refresh token: renew by signing a new
assertion. Every issuance and resource admission checks live agent policy.
Session and client revocation take effect on existing access tokens as well.

Native Kit composition calls the same authenticated registration mechanism for
the assistant's existing stable agent. Kit receives configured resource and
issuer URLs and private-key files; Kit owns OAuth discovery, connection failures,
and token renewal. Bootstrap makes no HTTP request to an MCP resource or issuer.
Built-in runtime tools accept ordinary agent credentials and keep their existing
per-tool authorization. This does not add a public Platform MCP scope.

Platform MCP parity: no new tool is added. This is a machine credential bootstrap
contract, not a new dashboard workflow. Existing client listing and revocation
remain authoritative. Demo seed data is unchanged because no rendered data model
or dashboard page changes, and the shared demo must not contain usable machine
private keys.
