# UpdateEnvironmentRequest

## Example Usage

```typescript
import { UpdateEnvironmentRequest } from "@gram/client/models/operations";

let value: UpdateEnvironmentRequest = {
  slug: "<value>",
  updateEnvironmentRequestBody: {
    entriesToRemove: [
      "<value>",
    ],
    entriesToUpdate: [
      {
        name: "<value>",
        value: "<value>",
      },
    ],
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `slug`                                                                                             | *string*                                                                                           | :heavy_check_mark:                                                                                 | The slug of the environment to update                                                              |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `updateEnvironmentRequestBody`                                                                     | [components.UpdateEnvironmentRequestBody](../../models/components/updateenvironmentrequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |