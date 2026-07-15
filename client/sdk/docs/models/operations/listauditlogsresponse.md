# ListAuditLogsResponse

## Example Usage

```typescript
import { ListAuditLogsResponse } from "@gram/client/models/operations/listauditlogs.js";

let value: ListAuditLogsResponse = {
  result: {
    logs: [
      {
        action: "<value>",
        actorId: "<id>",
        actorType: "<value>",
        createdAt: new Date("2025-01-23T14:31:09.134Z"),
        id: "<id>",
        subjectId: "<id>",
        subjectType: "<value>",
      },
    ],
  },
};
```

## Fields

| Field    | Type                                                                             | Required           | Description |
| -------- | -------------------------------------------------------------------------------- | ------------------ | ----------- |
| `result` | [components.ListAuditLogsResult](../../models/components/listauditlogsresult.md) | :heavy_check_mark: | N/A         |
