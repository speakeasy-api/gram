# GetMetricsSummaryPayload

Payload for getting metrics summary

## Example Usage

```typescript
import { GetMetricsSummaryPayload } from "@gram/client/models/components";

let value: GetMetricsSummaryPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  scope: "project",
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `chatId`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | Chat/conversation ID (required when scope=chat)                                               |                                                                                               |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start time in ISO 8601 format                                                                 | 2025-12-19T10:00:00Z                                                                          |
| `scope`                                                                                       | [components.Scope](../../models/components/scope.md)                                          | :heavy_check_mark:                                                                            | Aggregation scope for metrics                                                                 |                                                                                               |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End time in ISO 8601 format                                                                   | 2025-12-19T11:00:00Z                                                                          |