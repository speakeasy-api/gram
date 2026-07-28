# RemoteSessionIssuer

A remote_session_issuer record — upstream Authorization Server identity that Gram speaks OAuth to.

## Example Usage

```typescript
import { RemoteSessionIssuer } from "@gram/client/models/components/remotesessionissuer.js";

let value: RemoteSessionIssuer = {
  clientIdMetadataDocumentSupported: false,
  createdAt: new Date("2026-10-20T01:31:18.106Z"),
  id: "693f2802-f0be-4e6a-9251-b705728b6a02",
  issuer: "visa",
  oidc: false,
  organizationId: "<id>",
  passthrough: true,
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2025-05-28T09:33:08.214Z"),
};
```

## Fields

| Field                               | Type                                                                                          | Required           | Description                                                                                   |
| ----------------------------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------------------- |
| `authorizationEndpoint`             | _string_                                                                                      | :heavy_minus_sign: | Upstream authorization endpoint.                                                              |
| `clientIdMetadataDocumentSupported` | _boolean_                                                                                     | :heavy_check_mark: | Whether the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft). |
| `createdAt`                         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                                                                           |
| `grantTypesSupported`               | _string_[]                                                                                    | :heavy_minus_sign: | N/A                                                                                           |
| `id`                                | _string_                                                                                      | :heavy_check_mark: | The remote_session_issuer id.                                                                 |
| `issuer`                            | _string_                                                                                      | :heavy_check_mark: | Issuer URL; matches the iss claim.                                                            |
| `jwksUri`                           | _string_                                                                                      | :heavy_minus_sign: | Upstream JWKS URI; null when not advertised.                                                  |
| `logoAssetId`                       | _string_                                                                                      | :heavy_minus_sign: | Optional logo asset id; null when unset.                                                      |
| `name`                              | _string_                                                                                      | :heavy_minus_sign: | Optional display name; null when unset.                                                       |
| `oidc`                              | _boolean_                                                                                     | :heavy_check_mark: | When true, may unlock OIDC-aware behaviour.                                                   |
| `organizationId`                    | _string_                                                                                      | :heavy_check_mark: | The owning organization id. Empty for legacy rows not yet backfilled.                         |
| `passthrough`                       | _boolean_                                                                                     | :heavy_check_mark: | When true, the MCP client registers and transacts directly with this issuer.                  |
| `projectId`                         | _string_                                                                                      | :heavy_check_mark: | The owning project id. Empty for organization-level issuers.                                  |
| `registrationEndpoint`              | _string_                                                                                      | :heavy_minus_sign: | Upstream RFC 7591 registration endpoint; null for issuers without DCR.                        |
| `responseTypesSupported`            | _string_[]                                                                                    | :heavy_minus_sign: | N/A                                                                                           |
| `scopesSupported`                   | _string_[]                                                                                    | :heavy_minus_sign: | N/A                                                                                           |
| `slug`                              | _string_                                                                                      | :heavy_check_mark: | Project-unique slug.                                                                          |
| `tokenEndpoint`                     | _string_                                                                                      | :heavy_minus_sign: | Upstream token endpoint.                                                                      |
| `tokenEndpointAuthMethodsSupported` | _string_[]                                                                                    | :heavy_minus_sign: | N/A                                                                                           |
| `updatedAt`                         | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                                                                           |
