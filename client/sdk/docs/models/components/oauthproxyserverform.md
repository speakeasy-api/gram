# OAuthProxyServerForm

## Example Usage

```typescript
import { OAuthProxyServerForm } from "@gram/client/models/components/oauthproxyserverform.js";

let value: OAuthProxyServerForm = {
  providerType: "gram",
  slug: "<value>",
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `audience`                                                                                                 | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The audience parameter to send to the upstream OAuth provider                                              |
| `authorizationEndpoint`                                                                                    | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The authorization endpoint URL                                                                             |
| `environmentSlug`                                                                                          | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | A short url-friendly label that uniquely identifies a resource.                                            |
| `providerType`                                                                                             | [components.OAuthProxyServerFormProviderType](../../models/components/oauthproxyserverformprovidertype.md) | :heavy_check_mark:                                                                                         | The type of OAuth provider                                                                                 |
| `scopesSupported`                                                                                          | *string*[]                                                                                                 | :heavy_minus_sign:                                                                                         | OAuth scopes to request                                                                                    |
| `slug`                                                                                                     | *string*                                                                                                   | :heavy_check_mark:                                                                                         | A short url-friendly label that uniquely identifies a resource.                                            |
| `tokenEndpoint`                                                                                            | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The token endpoint URL                                                                                     |
| `tokenEndpointAuthMethodsSupported`                                                                        | *string*[]                                                                                                 | :heavy_minus_sign:                                                                                         | Auth methods (client_secret_basic or client_secret_post)                                                   |