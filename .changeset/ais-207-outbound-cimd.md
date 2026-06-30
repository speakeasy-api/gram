---
"server": minor
---

Add outbound OAuth Client ID Metadata Document (CIMD) support to remote-session OAuth. A `remote_session_client` can now be created in CIMD mode via a dedicated `remoteSessionClients.createCimd` endpoint: Gram generates the `client_id`, hosts a public client metadata document at `/.well-known/oauth-client/{id}`, and sends that platform-canonical URL as the `client_id` on every outbound `/authorize`, `/token`, and refresh call, with no symmetric secret and `token_endpoint_auth_method=none`. Issuer discovery now parses and persists `client_id_metadata_document_supported`, which gates the createCimd endpoint. The document endpoint is pinned to the platform host (404 on custom domains) so a strict upstream AS only ever validates the canonical URL. New management surface: the `createCimd` endpoint, `client_id_metadata_uri` on the client view, and the issuer CIMD-support flag on the issuer forms/views.
