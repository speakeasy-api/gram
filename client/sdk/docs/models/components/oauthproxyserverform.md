# OAuthProxyServerForm

## Example Usage

```typescript
import { OAuthProxyServerForm } from "@gram/client/models/components";

let value: OAuthProxyServerForm = {
  authorizationEndpoint: "<value>",
  environmentSlug: "<value>",
  scopesSupported: [
    "<value 1>",
    "<value 2>",
  ],
  slug: "<value>",
  tokenEndpoint: "<value>",
  tokenEndpointAuthMethodsSupported: [],
};
```

## Fields

| Field                                                           | Type                                                            | Required                                                        | Description                                                     |
| --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- | --------------------------------------------------------------- |
| `authorizationEndpoint`                                         | *string*                                                        | :heavy_check_mark:                                              | The authorization endpoint URL                                  |
| `environmentSlug`                                               | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |
| `scopesSupported`                                               | *string*[]                                                      | :heavy_check_mark:                                              | OAuth scopes to request                                         |
| `slug`                                                          | *string*                                                        | :heavy_check_mark:                                              | A short url-friendly label that uniquely identifies a resource. |
| `tokenEndpoint`                                                 | *string*                                                        | :heavy_check_mark:                                              | The token endpoint URL                                          |
| `tokenEndpointAuthMethodsSupported`                             | *string*[]                                                      | :heavy_check_mark:                                              | Auth methods (client_secret_basic or client_secret_post)        |