# QueryRequest

## Example Usage

```typescript
import { QueryRequest } from "@gram/client/models/operations/query.js";

let value: QueryRequest = {
  queryPayload: {
    from: new Date("2025-12-19T10:00:00Z"),
    groupBy: "department_name",
    to: new Date("2025-12-26T10:00:00Z"),
  },
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `gramSession`                                                      | *string*                                                           | :heavy_minus_sign:                                                 | Session header                                                     |
| `queryPayload`                                                     | [components.QueryPayload](../../models/components/querypayload.md) | :heavy_check_mark:                                                 | N/A                                                                |