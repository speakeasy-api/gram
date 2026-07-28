# UpdateOAuthProxyServerRequest

## Example Usage

```typescript
import { UpdateOAuthProxyServerRequest } from "@gram/client/models/operations/updateoauthproxyserver.js";

let value: UpdateOAuthProxyServerRequest = {
  slug: "<value>",
  updateOAuthProxyServerRequestBody: {
    oauthProxyServer: {},
  },
};
```

## Fields

| Field                               | Type                                                                                                         | Required           | Description                                                |
| ----------------------------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------- |
| `slug`                              | _string_                                                                                                     | :heavy_check_mark: | The slug of the toolset whose OAuth proxy server to update |
| `gramSession`                       | _string_                                                                                                     | :heavy_minus_sign: | Session header                                             |
| `gramKey`                           | _string_                                                                                                     | :heavy_minus_sign: | API Key header                                             |
| `gramProject`                       | _string_                                                                                                     | :heavy_minus_sign: | project header                                             |
| `updateOAuthProxyServerRequestBody` | [components.UpdateOAuthProxyServerRequestBody](../../models/components/updateoauthproxyserverrequestbody.md) | :heavy_check_mark: | N/A                                                        |
