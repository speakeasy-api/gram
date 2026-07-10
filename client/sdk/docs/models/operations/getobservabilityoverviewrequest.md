# GetObservabilityOverviewRequest

## Example Usage

```typescript
import { GetObservabilityOverviewRequest } from "@gram/client/models/operations/getobservabilityoverview.js";

let value: GetObservabilityOverviewRequest = {
  getObservabilityOverviewPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
};
```

## Fields

| Field                                                                                                    | Type                                                                                                     | Required                                                                                                 | Description                                                                                              |
| -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | API Key header                                                                                           |
| `gramSession`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | Session header                                                                                           |
| `gramProject`                                                                                            | *string*                                                                                                 | :heavy_minus_sign:                                                                                       | project header                                                                                           |
| `getObservabilityOverviewPayload`                                                                        | [components.GetObservabilityOverviewPayload](../../models/components/getobservabilityoverviewpayload.md) | :heavy_check_mark:                                                                                       | N/A                                                                                                      |