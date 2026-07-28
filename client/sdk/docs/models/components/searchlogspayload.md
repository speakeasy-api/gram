# SearchLogsPayload

Payload for searching telemetry logs

## Example Usage

```typescript
import { SearchLogsPayload } from "@gram/client/models/components/searchlogspayload.js";

let value: SearchLogsPayload = {
  filter: {
    from: new Date("2025-12-19T10:00:00Z"),
    to: new Date("2025-12-19T11:00:00Z"),
  },
  filters: [
    {
      path: "@user.region",
    },
  ],
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field     | Type                                                                                          | Required           | Description                                                  | Example              |
| --------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------ | -------------------- |
| `cursor`  | _string_                                                                                      | :heavy_minus_sign: | Cursor for pagination                                        |                      |
| `filter`  | [components.SearchLogsFilter](../../models/components/searchlogsfilter.md)                    | :heavy_minus_sign: | Filter criteria for searching logs                           |                      |
| `filters` | [components.LogFilter](../../models/components/logfilter.md)[]                                | :heavy_minus_sign: | Filter conditions for the search query                       |                      |
| `from`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z') | 2025-12-19T10:00:00Z |
| `limit`   | _number_                                                                                      | :heavy_minus_sign: | Number of items to return (1-1000)                           |                      |
| `sort`    | [components.SearchLogsPayloadSort](../../models/components/searchlogspayloadsort.md)          | :heavy_minus_sign: | Sort order                                                   |                      |
| `to`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')   | 2025-12-19T11:00:00Z |
