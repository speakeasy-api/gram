# UpdateExternalOAuthServerRequest

## Example Usage

```typescript
import { UpdateExternalOAuthServerRequest } from "@gram/client/models/operations";

let value: UpdateExternalOAuthServerRequest = {
  slug: "<value>",
  updateExternalOAuthServerRequestBody: {
    metadata: "<value>",
  },
};
```

## Fields

| Field                                                                                                              | Type                                                                                                               | Required                                                                                                           | Description                                                                                                        |
| ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `slug`                                                                                                             | *string*                                                                                                           | :heavy_check_mark:                                                                                                 | The slug of the toolset to update                                                                                  |
| `gramSession`                                                                                                      | *string*                                                                                                           | :heavy_minus_sign:                                                                                                 | Session header                                                                                                     |
| `gramKey`                                                                                                          | *string*                                                                                                           | :heavy_minus_sign:                                                                                                 | API Key header                                                                                                     |
| `gramProject`                                                                                                      | *string*                                                                                                           | :heavy_minus_sign:                                                                                                 | project header                                                                                                     |
| `updateExternalOAuthServerRequestBody`                                                                             | [components.UpdateExternalOAuthServerRequestBody](../../models/components/updateexternaloauthserverrequestbody.md) | :heavy_check_mark:                                                                                                 | N/A                                                                                                                |