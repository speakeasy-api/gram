# Agent OAuth clients

An ordinary agent can register a renewable asymmetric OAuth client through the
existing resource registration endpoint. This does not require an assistant,
runtime, trigger, or workload issuer.

Use HTTPS for registration and token requests; never transmit the agent API key
or signed client assertion over plaintext HTTP.

Send `POST /mcp/<SLUG>/register` with `Authorization: Bearer <AGENT_API_KEY>` and
`Content-Type: application/json`:

```json
{
  "client_name": "Native agent",
  "agent_urn": "agent:<AGENT_ID>",
  "grant_types": ["client_credentials"],
  "token_endpoint_auth_method": "private_key_jwt",
  "jwks": { "keys": [{ "kty": "RSA", "n": "<PUBLIC_MODULUS>", "e": "AQAB" }] }
}
```

Replace the example public JWK with your public JWK object. The agent URN must
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

Runtime turn credentials cannot register independent OAuth clients: doing so
would discard their runtime binding and lifetime. Built-in runtime tools retain
the existing runtime credential binding checks. Ordinary agent API keys may
select a project explicitly and remain subject to per-tool authorization.
This does not add a public Platform MCP scope.

Platform MCP parity: no new tool is added. This is a machine credential bootstrap
contract, not a new dashboard workflow. Existing client listing and revocation
remain authoritative. Demo seed data is unchanged because no rendered data model
or dashboard page changes, and the shared demo must not contain usable machine
private keys.
