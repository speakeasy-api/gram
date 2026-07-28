# RemoteSessionIssuerDraft

A draft remote_session_issuer returned by discover. Same shape as RemoteSessionIssuer minus id/project_id/timestamps, plus discovery_warnings describing any RFC 8414 deviations.

## Example Usage

```typescript
import { RemoteSessionIssuerDraft } from "@gram/client/models/components/remotesessionissuerdraft.js";

let value: RemoteSessionIssuerDraft = {
  clientIdMetadataDocumentSupported: false,
  discoveryWarnings: ["<value 1>", "<value 2>", "<value 3>"],
  issuer: "diners_club",
  oidc: true,
  passthrough: false,
};
```

## Fields

| Field                               | Type       | Required           | Description                                                                                                                                      |
| ----------------------------------- | ---------- | ------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------ |
| `authorizationEndpoint`             | _string_   | :heavy_minus_sign: | Upstream authorization endpoint.                                                                                                                 |
| `clientIdMetadataDocumentSupported` | _boolean_  | :heavy_check_mark: | Whether the issuer advertises support for a Client ID Metadata Document URL as client_id (OAuth CIMD draft), parsed from the discovery document. |
| `discoveryWarnings`                 | _string_[] | :heavy_check_mark: | Warnings describing any RFC 8414 deviations encountered during discovery.                                                                        |
| `grantTypesSupported`               | _string_[] | :heavy_minus_sign: | N/A                                                                                                                                              |
| `issuer`                            | _string_   | :heavy_check_mark: | Issuer URL; matches the iss claim.                                                                                                               |
| `jwksUri`                           | _string_   | :heavy_minus_sign: | Upstream JWKS URI; null when not advertised.                                                                                                     |
| `oidc`                              | _boolean_  | :heavy_check_mark: | When true, may unlock OIDC-aware behaviour.                                                                                                      |
| `passthrough`                       | _boolean_  | :heavy_check_mark: | When true, the MCP client registers and transacts directly with this issuer.                                                                     |
| `registrationEndpoint`              | _string_   | :heavy_minus_sign: | Upstream RFC 7591 registration endpoint; null for issuers without DCR.                                                                           |
| `responseTypesSupported`            | _string_[] | :heavy_minus_sign: | N/A                                                                                                                                              |
| `scopesSupported`                   | _string_[] | :heavy_minus_sign: | N/A                                                                                                                                              |
| `tokenEndpoint`                     | _string_   | :heavy_minus_sign: | Upstream token endpoint.                                                                                                                         |
| `tokenEndpointAuthMethodsSupported` | _string_[] | :heavy_minus_sign: | N/A                                                                                                                                              |
