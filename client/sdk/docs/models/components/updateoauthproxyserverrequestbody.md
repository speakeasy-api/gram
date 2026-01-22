# UpdateOAuthProxyServerRequestBody

## Example Usage

```typescript
import { UpdateOAuthProxyServerRequestBody } from "@gram/client/models/components";

let value: UpdateOAuthProxyServerRequestBody = {};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `authorizationEndpoint`                                  | *string*                                                 | :heavy_minus_sign:                                       | The authorization endpoint URL                           |
| `environmentSlug`                                        | *string*                                                 | :heavy_minus_sign:                                       | The environment slug to store secrets                    |
| `scopesSupported`                                        | *string*[]                                               | :heavy_minus_sign:                                       | OAuth scopes to request                                  |
| `tokenEndpoint`                                          | *string*                                                 | :heavy_minus_sign:                                       | The token endpoint URL                                   |
| `tokenEndpointAuthMethodsSupported`                      | *string*[]                                               | :heavy_minus_sign:                                       | Auth methods (client_secret_basic or client_secret_post) |