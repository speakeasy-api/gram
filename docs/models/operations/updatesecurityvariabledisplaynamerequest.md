# UpdateSecurityVariableDisplayNameRequest

## Example Usage

```typescript
import { UpdateSecurityVariableDisplayNameRequest } from "@gram/client/models/operations";

let value: UpdateSecurityVariableDisplayNameRequest = {
  updateSecurityVariableDisplayNameRequestBody: {
    displayName: "Kelly_McDermott74",
    securityKey: "<value>",
    toolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                                                              | Type                                                                                                                               | Required                                                                                                                           | Description                                                                                                                        |
| ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                                      | *string*                                                                                                                           | :heavy_minus_sign:                                                                                                                 | Session header                                                                                                                     |
| `gramKey`                                                                                                                          | *string*                                                                                                                           | :heavy_minus_sign:                                                                                                                 | API Key header                                                                                                                     |
| `gramProject`                                                                                                                      | *string*                                                                                                                           | :heavy_minus_sign:                                                                                                                 | project header                                                                                                                     |
| `updateSecurityVariableDisplayNameRequestBody`                                                                                     | [components.UpdateSecurityVariableDisplayNameRequestBody](../../models/components/updatesecurityvariabledisplaynamerequestbody.md) | :heavy_check_mark:                                                                                                                 | N/A                                                                                                                                |