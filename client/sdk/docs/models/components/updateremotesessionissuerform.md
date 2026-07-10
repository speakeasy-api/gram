# UpdateRemoteSessionIssuerForm

Form for updating a remote_session_issuer. All non-id fields are optional patches.

## Example Usage

```typescript
import { UpdateRemoteSessionIssuerForm } from "@gram/client/models/components/updateremotesessionissuerform.js";

let value: UpdateRemoteSessionIssuerForm = {
  id: "67f40b9b-9b2a-4b4e-b44a-1ce7a1718297",
};
```

## Fields

| Field                               | Type       | Required           | Description                                                                                   |
| ----------------------------------- | ---------- | ------------------ | --------------------------------------------------------------------------------------------- |
| `authorizationEndpoint`             | _string_   | :heavy_minus_sign: | Upstream authorization endpoint.                                                              |
| `clientIdMetadataDocumentSupported` | _boolean_  | :heavy_minus_sign: | Whether the issuer accepts a Client ID Metadata Document URL as client_id (OAuth CIMD draft). |
| `grantTypesSupported`               | _string_[] | :heavy_minus_sign: | N/A                                                                                           |
| `id`                                | _string_   | :heavy_check_mark: | The remote_session_issuer id.                                                                 |
| `issuer`                            | _string_   | :heavy_minus_sign: | Issuer URL; matches the iss claim.                                                            |
| `jwksUri`                           | _string_   | :heavy_minus_sign: | Upstream JWKS URI.                                                                            |
| `logoAssetId`                       | _string_   | :heavy_minus_sign: | Set the logo asset id.                                                                        |
| `name`                              | _string_   | :heavy_minus_sign: | Set or clear the display name. An empty string clears it to NULL.                             |
| `oidc`                              | _boolean_  | :heavy_minus_sign: | N/A                                                                                           |
| `passthrough`                       | _boolean_  | :heavy_minus_sign: | N/A                                                                                           |
| `registrationEndpoint`              | _string_   | :heavy_minus_sign: | Upstream RFC 7591 registration endpoint.                                                      |
| `responseTypesSupported`            | _string_[] | :heavy_minus_sign: | N/A                                                                                           |
| `scopesSupported`                   | _string_[] | :heavy_minus_sign: | N/A                                                                                           |
| `slug`                              | _string_   | :heavy_minus_sign: | Rename the slug.                                                                              |
| `tokenEndpoint`                     | _string_   | :heavy_minus_sign: | Upstream token endpoint.                                                                      |
| `tokenEndpointAuthMethodsSupported` | _string_[] | :heavy_minus_sign: | N/A                                                                                           |
