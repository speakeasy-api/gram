# GetToolUsageFilterOptionsRequest

## Example Usage

```typescript
import { GetToolUsageFilterOptionsRequest } from "@gram/client/models/operations/gettoolusagefilteroptions.js";

let value: GetToolUsageFilterOptionsRequest = {
  getToolUsageFilterOptionsPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field                                                                                                      | Type                                                                                                       | Required                                                                                                   | Description                                                                                                |
| ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                  | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | API Key header                                                                                             |
| `gramSession`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | Session header                                                                                             |
| `gramProject`                                                                                              | *string*                                                                                                   | :heavy_minus_sign:                                                                                         | project header                                                                                             |
| `getToolUsageFilterOptionsPayload`                                                                         | [components.GetToolUsageFilterOptionsPayload](../../models/components/gettoolusagefilteroptionspayload.md) | :heavy_check_mark:                                                                                         | N/A                                                                                                        |