# ListFilterOptionsPayload

Payload for listing filter options

## Example Usage

```typescript
import { ListFilterOptionsPayload } from "@gram/client/models/components/listfilteroptionspayload.js";

let value: ListFilterOptionsPayload = {
  filterType: "agent",
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   | Example                                                                                       |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `eventSource`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Optional event source filter for the option list                                              |                                                                                               |
| `filterType`                                                                                  | [components.FilterType](../../models/components/filtertype.md)                                | :heavy_check_mark:                                                                            | Type of filter to list options for                                                            |                                                                                               |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start time in ISO 8601 format                                                                 | 2025-12-19T10:00:00Z                                                                          |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End time in ISO 8601 format                                                                   | 2025-12-19T11:00:00Z                                                                          |