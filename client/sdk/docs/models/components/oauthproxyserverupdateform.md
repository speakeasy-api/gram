# OAuthProxyServerUpdateForm

## Example Usage

```typescript
import { OAuthProxyServerUpdateForm } from "@gram/client/models/components/oauthproxyserverupdateform.js";

let value: OAuthProxyServerUpdateForm = {};
```

## Fields

| Field                               | Type       | Required           | Description                                                     |
| ----------------------------------- | ---------- | ------------------ | --------------------------------------------------------------- |
| `audience`                          | _string_   | :heavy_minus_sign: | The audience parameter to send to the upstream OAuth provider   |
| `authorizationEndpoint`             | _string_   | :heavy_minus_sign: | The authorization endpoint URL                                  |
| `environmentSlug`                   | _string_   | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource. |
| `scopesSupported`                   | _string_[] | :heavy_minus_sign: | OAuth scopes to request (omit = no change, empty array = clear) |
| `tokenEndpoint`                     | _string_   | :heavy_minus_sign: | The token endpoint URL                                          |
| `tokenEndpointAuthMethodsSupported` | _string_[] | :heavy_minus_sign: | Auth methods (omit = no change, empty array = clear)            |
