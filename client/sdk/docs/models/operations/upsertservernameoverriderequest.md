# UpsertServerNameOverrideRequest

## Example Usage

```typescript
import { UpsertServerNameOverrideRequest } from "@gram/client/models/operations/upsertservernameoverride.js";

let value: UpsertServerNameOverrideRequest = {
  upsertRequestBody: {
    displayName: "Sandrine_Hoppe54",
    rawServerName: "<value>",
  },
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `gramKey`                                                                    | *string*                                                                     | :heavy_minus_sign:                                                           | API Key header                                                               |
| `gramSession`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | Session header                                                               |
| `gramProject`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | project header                                                               |
| `upsertRequestBody`                                                          | [components.UpsertRequestBody](../../models/components/upsertrequestbody.md) | :heavy_check_mark:                                                           | N/A                                                                          |