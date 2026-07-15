# ListSessionsPayload

Payload for listing org-scoped chat sessions

## Example Usage

```typescript
import { ListSessionsPayload } from "@gram/client/models/components/listsessionspayload.js";

let value: ListSessionsPayload = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-26T10:00:00Z"),
};
```

## Fields

| Field     | Type                                                                                          | Required           | Description                                            | Example              |
| --------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------ | -------------------- |
| `cursor`  | _string_                                                                                      | :heavy_minus_sign: | Opaque cursor for pagination                           |                      |
| `filters` | [components.QueryFilter](../../models/components/queryfilter.md)[]                            | :heavy_minus_sign: | Optional filters; all filters are ANDed together.      |                      |
| `from`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | Start time in ISO 8601 format                          | 2025-12-19T10:00:00Z |
| `limit`   | _number_                                                                                      | :heavy_minus_sign: | Number of sessions to return (1-1000)                  |                      |
| `sortBy`  | [components.SortBy](../../models/components/sortby.md)                                        | :heavy_minus_sign: | Measure used to rank sessions. Defaults to total_cost. |                      |
| `to`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | End time in ISO 8601 format                            | 2025-12-26T10:00:00Z |
