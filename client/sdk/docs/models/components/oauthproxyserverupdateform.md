# OAuthProxyServerUpdateForm

## Example Usage

```typescript
import { OAuthProxyServerUpdateForm } from "@gram/client/models/components/oauthproxyserverupdateform.js";

let value: OAuthProxyServerUpdateForm = {};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `audience`                                                      | *string*                                                        | :heavy_minus_sign:                                              | The audience parameter to send to the upstream OAuth provider   |
| `authorizationEndpoint`                                         | *string*                                                        | :heavy_minus_sign:                                              | The authorization endpoint URL                                  |
| `environmentSlug`                                               | *string*                                                        | :heavy_minus_sign:                                              | A short url-friendly label that uniquely identifies a resource. |
| `scopesSupported`                                               | *string*[]                                                      | :heavy_minus_sign:                                              | OAuth scopes to request (omit = no change, empty array = clear) |
| `tokenEndpoint`                                                 | *string*                                                        | :heavy_minus_sign:                                              | The token endpoint URL                                          |
| `tokenEndpointAuthMethodsSupported`                             | *string*[]                                                      | :heavy_minus_sign:                                              | Auth methods (omit = no change, empty array = clear)            |