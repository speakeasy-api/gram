# GetProjectMetricsSummaryPayload

Payload for getting project-level metrics summary

## Example Usage

```typescript
import { GetProjectMetricsSummaryPayload } from "@gram/client/models/components";

let value: GetProjectMetricsSummaryPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start time in ISO 8601 format                                                                 | 2025-12-19T10:00:00Z                                                                          |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End time in ISO 8601 format                                                                   | 2025-12-19T11:00:00Z                                                                          |