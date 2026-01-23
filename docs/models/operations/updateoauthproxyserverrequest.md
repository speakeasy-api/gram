# UpdateOAuthProxyServerRequest

## Example Usage

```typescript
import { UpdateOAuthProxyServerRequest } from "@gram/client/models/operations";

let value: UpdateOAuthProxyServerRequest = {
  slug: "<value>",
  updateOAuthProxyServerRequestBody: {},
};
```

## Fields

| Field                                                                                                        | Type                                                                                                         | Required                                                                                                     | Description                                                                                                  |
| ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| `slug`                                                                                                       | *string*                                                                                                     | :heavy_check_mark:                                                                                           | The slug of the toolset to update                                                                            |
| `gramSession`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | Session header                                                                                               |
| `gramKey`                                                                                                    | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | API Key header                                                                                               |
| `gramProject`                                                                                                | *string*                                                                                                     | :heavy_minus_sign:                                                                                           | project header                                                                                               |
| `updateOAuthProxyServerRequestBody`                                                                          | [components.UpdateOAuthProxyServerRequestBody](../../models/components/updateoauthproxyserverrequestbody.md) | :heavy_check_mark:                                                                                           | N/A                                                                                                          |