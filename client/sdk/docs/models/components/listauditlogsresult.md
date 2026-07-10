# ListAuditLogsResult

## Example Usage

```typescript
import { ListAuditLogsResult } from "@gram/client/models/components/listauditlogsresult.js";

let value: ListAuditLogsResult = {
  logs: [],
};
```

## Fields

| Field        | Type                                                         | Required           | Description                                         |
| ------------ | ------------------------------------------------------------ | ------------------ | --------------------------------------------------- |
| `logs`       | [components.AuditLog](../../models/components/auditlog.md)[] | :heavy_check_mark: | List of audit logs                                  |
| `nextCursor` | _string_                                                     | :heavy_minus_sign: | The cursor to be used for the next page of results. |
