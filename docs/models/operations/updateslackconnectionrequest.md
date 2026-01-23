# UpdateSlackConnectionRequest

## Example Usage

```typescript
import { UpdateSlackConnectionRequest } from "@gram/client/models/operations";

let value: UpdateSlackConnectionRequest = {
  updateSlackConnectionRequestBody: {
    defaultToolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Session header                                                                                             |
| `gramProject`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | project header                                                                                             |
| `updateSlackConnectionRequestBody`                                                                         | [components.UpdateSlackConnectionRequestBody](../../models/components/updateslackconnectionrequestbody.md) | :heavy_check_mark:                                                                                         | N/A                                                                                                        |