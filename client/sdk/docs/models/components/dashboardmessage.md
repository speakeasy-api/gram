# DashboardMessage

## Example Usage

```typescript
import { DashboardMessage } from "@gram/client/models/components";

let value: DashboardMessage = {
  content: "<value>",
  createdAt: new Date("2025-06-25T10:43:39.881Z"),
  id: "eb372465-741d-442f-bc5b-2ed4e58d8eb9",
  role: "assistant",
  seq: 607745,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `content`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Message content (Markdown).                                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | RFC3339 creation timestamp.                                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Message id.                                                                                   |
| `role`                                                                                        | [components.DashboardMessageRole](../../models/components/dashboardmessagerole.md)            | :heavy_check_mark:                                                                            | Message author.                                                                               |
| `seq`                                                                                         | *number*                                                                                      | :heavy_check_mark:                                                                            | Monotonic cursor; pass the latest value as after_seq to poll for newer messages.              |