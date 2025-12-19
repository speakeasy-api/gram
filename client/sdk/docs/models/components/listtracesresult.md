# ListTracesResult

Result of listing trace summaries

## Example Usage

```typescript
import { ListTracesResult } from "@gram/client/models/components";

let value: ListTracesResult = {
  traces: [],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | Cursor for next page (trace ID)                                                  |
| `traces`                                                                         | [components.TraceSummaryRecord](../../models/components/tracesummaryrecord.md)[] | :heavy_check_mark:                                                               | List of trace summaries                                                          |