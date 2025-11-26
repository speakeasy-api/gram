# AddOAuthProxyServerRequestBody

## Example Usage

```typescript
import { AddOAuthProxyServerRequestBody } from "@gram/client/models/components";

let value: AddOAuthProxyServerRequestBody = {
  oauthProxyServer: {
    authorizationEndpoint: "<value>",
    environmentSlug: "<value>",
    scopesSupported: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    slug: "<value>",
    tokenEndpoint: "<value>",
    tokenEndpointAuthMethodsSupported: [
      "<value 1>",
      "<value 2>",
    ],
  },
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `oauthProxyServer`                                                                 | [components.OAuthProxyServerForm](../../models/components/oauthproxyserverform.md) | :heavy_check_mark:                                                                 | N/A                                                                                |