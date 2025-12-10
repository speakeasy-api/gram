# OAuthProxyServerForm

## Example Usage

```typescript
import { OAuthProxyServerForm } from "@gram/client/models/components";

let value: OAuthProxyServerForm = {
  environmentSlug: "<value>",
  providerType: "gram",
  slug: "<value>",
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `authorizationEndpoint`                                                                                    | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The authorization endpoint URL                                                                             |
| `environmentSlug`                                                                                          | *string*                                                                                                   | :heavy_check_mark:                                                                                         | A short url-friendly label that uniquely identifies a resource.                                            |
| `providerType`                                                                                             | [components.OAuthProxyServerFormProviderType](../../models/components/oauthproxyserverformprovidertype.md) | :heavy_check_mark:                                                                                         | The type of OAuth provider                                                                                 |
| `scopesSupported`                                                                                          | *string*[]                                                                                                 | :heavy_minus_sign:                                                                                         | OAuth scopes to request                                                                                    |
| `slug`                                                                                                     | *string*                                                                                                   | :heavy_check_mark:                                                                                         | A short url-friendly label that uniquely identifies a resource.                                            |
| `tokenEndpoint`                                                                                            | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | The token endpoint URL                                                                                     |
| `tokenEndpointAuthMethodsSupported`                                                                        | *string*[]                                                                                                 | :heavy_minus_sign:                                                                                         | Auth methods (client_secret_basic or client_secret_post)                                                   |