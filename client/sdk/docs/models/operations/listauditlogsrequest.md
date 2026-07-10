# ListAuditLogsRequest

## Example Usage

```typescript
import { ListAuditLogsRequest } from "@gram/client/models/operations/listauditlogs.js";

let value: ListAuditLogsRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                                                                                                                           |
| ------------- | -------- | ------------------ | ----------------------------------------------------------------------------------------------------------------------------------------------------- |
| `cursor`      | _string_ | :heavy_minus_sign: | The cursor for paginating through audit logs.                                                                                                         |
| `projectSlug` | _string_ | :heavy_minus_sign: | Project slug to filter audit logs to a specific project.                                                                                              |
| `actorId`     | _string_ | :heavy_minus_sign: | Actor ID to filter audit logs to a specific actor.                                                                                                    |
| `action`      | _string_ | :heavy_minus_sign: | Action to filter audit logs to a specific action.                                                                                                     |
| `subjectType` | _string_ | :heavy_minus_sign: | Subject type to filter audit logs to a specific kind of subject. When omitted, assistant activity events are excluded; pass 'assistant' to list them. |
| `subjectId`   | _string_ | :heavy_minus_sign: | Subject ID to filter audit logs to a specific subject (e.g. a single assistant).                                                                      |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                                                                                                        |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                                                                                                        |
