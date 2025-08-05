# OAuthProxyProvider

## Example Usage

```typescript
import { OAuthProxyProvider } from "@gram/client/models/components";

let value: OAuthProxyProvider = {
  authorizationEndpoint: "<value>",
  createdAt: new Date("2023-11-23T21:56:04.000Z"),
  id: "<id>",
  slug: "<value>",
  tokenEndpoint: "<value>",
  updatedAt: new Date("2023-09-24T08:35:06.731Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `authorizationEndpoint`                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The authorization endpoint URL                                                                |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the OAuth proxy provider was created.                                                    |
| `grantTypesSupported`                                                                         | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The grant types supported by this provider                                                    |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the OAuth proxy provider                                                            |
| `scopesSupported`                                                                             | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The OAuth scopes supported by this provider                                                   |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `tokenEndpoint`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | The token endpoint URL                                                                        |
| `tokenEndpointAuthMethodsSupported`                                                           | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The token endpoint auth methods supported by this provider                                    |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the OAuth proxy provider was last updated.                                               |