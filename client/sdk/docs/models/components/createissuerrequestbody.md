# CreateIssuerRequestBody

## Example Usage

```typescript
import { CreateIssuerRequestBody } from "@gram/client/models/components/createissuerrequestbody.js";

let value: CreateIssuerRequestBody = {
  issuer: "visa",
  slug: "<value>",
};
```

## Fields

| Field                               | Type       | Required           | Description                                                                                                                                                                                        |
| ----------------------------------- | ---------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `authorizationEndpoint`             | _string_   | :heavy_minus_sign: | Upstream authorization endpoint.                                                                                                                                                                   |
| `clientIdMetadataDocumentSupported` | _boolean_  | :heavy_minus_sign: | When true, the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft). Discovered from the issuer metadata document and used to pre-flight outbound CIMD. Default false. |
| `grantTypesSupported`               | _string_[] | :heavy_minus_sign: | Grant types advertised by the issuer.                                                                                                                                                              |
| `issuer`                            | _string_   | :heavy_check_mark: | Issuer URL; matches the iss claim.                                                                                                                                                                 |
| `jwksUri`                           | _string_   | :heavy_minus_sign: | Upstream JWKS URI.                                                                                                                                                                                 |
| `logoAssetId`                       | _string_   | :heavy_minus_sign: | Optional logo asset id.                                                                                                                                                                            |
| `name`                              | _string_   | :heavy_minus_sign: | Optional display name. Stored NULL when empty; clients fall back to the issuer URL/slug.                                                                                                           |
| `oidc`                              | _boolean_  | :heavy_minus_sign: | When true, may unlock OIDC-aware behaviour. Default false.                                                                                                                                         |
| `passthrough`                       | _boolean_  | :heavy_minus_sign: | When true, the MCP client registers and transacts directly with this issuer. Default false.                                                                                                        |
| `projectId`                         | _string_   | :heavy_minus_sign: | Owning project id; the project must belong to the caller's organization. Omit to create an organization-level issuer.                                                                              |
| `registrationEndpoint`              | _string_   | :heavy_minus_sign: | Upstream RFC 7591 registration endpoint; absent for issuers without DCR.                                                                                                                           |
| `responseTypesSupported`            | _string_[] | :heavy_minus_sign: | Response types advertised by the issuer.                                                                                                                                                           |
| `scopesSupported`                   | _string_[] | :heavy_minus_sign: | Scopes advertised by the issuer.                                                                                                                                                                   |
| `slug`                              | _string_   | :heavy_check_mark: | Project-unique slug.                                                                                                                                                                               |
| `tokenEndpoint`                     | _string_   | :heavy_minus_sign: | Upstream token endpoint.                                                                                                                                                                           |
| `tokenEndpointAuthMethodsSupported` | _string_[] | :heavy_minus_sign: | Token endpoint auth methods advertised by the issuer.                                                                                                                                              |
