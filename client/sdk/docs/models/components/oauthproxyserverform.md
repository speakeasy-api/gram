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

| Field                               | Type                                                                                                       | Required           | Description                                                     |
| ----------------------------------- | ---------------------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------- |
| `audience`                          | _string_                                                                                                   | :heavy_minus_sign: | The audience parameter to send to the upstream OAuth provider   |
| `authorizationEndpoint`             | _string_                                                                                                   | :heavy_minus_sign: | The authorization endpoint URL                                  |
| `environmentSlug`                   | _string_                                                                                                   | :heavy_minus_sign: | A short url-friendly label that uniquely identifies a resource. |
| `providerType`                      | [components.OAuthProxyServerFormProviderType](../../models/components/oauthproxyserverformprovidertype.md) | :heavy_check_mark: | The type of OAuth provider                                      |
| `scopesSupported`                   | _string_[]                                                                                                 | :heavy_minus_sign: | OAuth scopes to request                                         |
| `slug`                              | _string_                                                                                                   | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource. |
| `tokenEndpoint`                     | _string_                                                                                                   | :heavy_minus_sign: | The token endpoint URL                                          |
| `tokenEndpointAuthMethodsSupported` | _string_[]                                                                                                 | :heavy_minus_sign: | Auth methods (client_secret_basic or client_secret_post)        |
