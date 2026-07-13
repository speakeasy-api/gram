# QueryTumDetailsRequest

## Example Usage

```typescript
import { QueryTumDetailsRequest } from "@gram/client/models/operations/querytumdetails.js";

let value: QueryTumDetailsRequest = {
  telemetryWindowPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-26T10:00:00Z"),
  },
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `gramSession`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | Session header                                                                         |
| `telemetryWindowPayload`                                                               | [components.TelemetryWindowPayload](../../models/components/telemetrywindowpayload.md) | :heavy_check_mark:                                                                     | N/A                                                                                    |