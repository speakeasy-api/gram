# GetMetricsSummaryRequest

## Example Usage

```typescript
import { GetMetricsSummaryRequest } from "@gram/client/models/operations";

let value: GetMetricsSummaryRequest = {
  getMetricsSummaryPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    scope: "chat",
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramKey`                                                                                  | *string*                                                                                   | :heavy_minus_sign:                                                                         | API Key header                                                                             |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `getMetricsSummaryPayload`                                                                 | [components.GetMetricsSummaryPayload](../../models/components/getmetricssummarypayload.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |