# AuditLog

## Example Usage

```typescript
import { AuditLog } from "@gram/client/models/components/auditlog.js";

let value: AuditLog = {
  action: "<value>",
  actorId: "<id>",
  actorType: "<value>",
  createdAt: new Date("2024-11-07T00:35:21.743Z"),
  id: "<id>",
  subjectId: "<id>",
  subjectType: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `action`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `actorDisplayName`                                                                            | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `actorId`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `actorSlug`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `actorType`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `afterSnapshot`                                                                               | *any*                                                                                         | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `beforeSnapshot`                                                                              | *any*                                                                                         | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the audit log.                                                           |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `metadata`                                                                                    | Record<string, *any*>                                                                         | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `projectSlug`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `subjectDisplayName`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `subjectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `subjectSlug`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | N/A                                                                                           |
| `subjectType`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | N/A                                                                                           |