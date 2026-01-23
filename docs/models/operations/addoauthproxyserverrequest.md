# AddOAuthProxyServerRequest

## Example Usage

```typescript
import { AddOAuthProxyServerRequest } from "@gram/client/models/operations";

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

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `slug`                                                                                                 | *string*                                                                                               | :heavy_check_mark:                                                                                     | The slug of the toolset to update                                                                      |
| `gramSession`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Session header                                                                                         |
| `gramKey`                                                                                              | *string*                                                                                               | :heavy_minus_sign:                                                                                     | API Key header                                                                                         |
| `gramProject`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | project header                                                                                         |
| `addOAuthProxyServerRequestBody`                                                                       | [components.AddOAuthProxyServerRequestBody](../../models/components/addoauthproxyserverrequestbody.md) | :heavy_check_mark:                                                                                     | N/A                                                                                                    |