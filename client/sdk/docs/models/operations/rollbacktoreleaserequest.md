# RollbackToReleaseRequest

## Example Usage

```typescript
import { RollbackToReleaseRequest } from "@gram/client/models/operations";

let value: RollbackToReleaseRequest = {
  rollbackToReleaseRequestBody: {
    releaseNumber: 798127,
    toolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                              | Type                                                                                               | Required                                                                                           | Description                                                                                        |
| -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | Session header                                                                                     |
| `gramKey`                                                                                          | *string*                                                                                           | :heavy_minus_sign:                                                                                 | API Key header                                                                                     |
| `gramProject`                                                                                      | *string*                                                                                           | :heavy_minus_sign:                                                                                 | project header                                                                                     |
| `rollbackToReleaseRequestBody`                                                                     | [components.RollbackToReleaseRequestBody](../../models/components/rollbacktoreleaserequestbody.md) | :heavy_check_mark:                                                                                 | N/A                                                                                                |