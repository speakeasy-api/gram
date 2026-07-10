# AddOAuthProxyServerRequest

## Example Usage

```typescript
import { AddOAuthProxyServerRequest } from "@gram/client/models/operations/addoauthproxyserver.js";

let value: AddOAuthProxyServerRequest = {
  slug: "<value>",
  addOAuthProxyServerRequestBody: {
    oauthProxyServer: {
      providerType: "gram",
      slug: "<value>",
    },
  },
};
```

## Fields

| Field                            | Type                                                                                                   | Required           | Description                       |
| -------------------------------- | ------------------------------------------------------------------------------------------------------ | ------------------ | --------------------------------- |
| `slug`                           | _string_                                                                                               | :heavy_check_mark: | The slug of the toolset to update |
| `gramSession`                    | _string_                                                                                               | :heavy_minus_sign: | Session header                    |
| `gramKey`                        | _string_                                                                                               | :heavy_minus_sign: | API Key header                    |
| `gramProject`                    | _string_                                                                                               | :heavy_minus_sign: | project header                    |
| `addOAuthProxyServerRequestBody` | [components.AddOAuthProxyServerRequestBody](../../models/components/addoauthproxyserverrequestbody.md) | :heavy_check_mark: | N/A                               |
