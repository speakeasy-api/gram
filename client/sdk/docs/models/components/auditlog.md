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

| Field                | Type                                                                                          | Required           | Description                         |
| -------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------- |
| `action`             | _string_                                                                                      | :heavy_check_mark: | N/A                                 |
| `actorDisplayName`   | _string_                                                                                      | :heavy_minus_sign: | N/A                                 |
| `actorId`            | _string_                                                                                      | :heavy_check_mark: | N/A                                 |
| `actorSlug`          | _string_                                                                                      | :heavy_minus_sign: | N/A                                 |
| `actorType`          | _string_                                                                                      | :heavy_check_mark: | N/A                                 |
| `afterSnapshot`      | _any_                                                                                         | :heavy_minus_sign: | N/A                                 |
| `beforeSnapshot`     | _any_                                                                                         | :heavy_minus_sign: | N/A                                 |
| `createdAt`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the audit log. |
| `id`                 | _string_                                                                                      | :heavy_check_mark: | N/A                                 |
| `metadata`           | Record<string, _any_>                                                                         | :heavy_minus_sign: | N/A                                 |
| `projectId`          | _string_                                                                                      | :heavy_minus_sign: | N/A                                 |
| `projectSlug`        | _string_                                                                                      | :heavy_minus_sign: | N/A                                 |
| `subjectDisplayName` | _string_                                                                                      | :heavy_minus_sign: | N/A                                 |
| `subjectId`          | _string_                                                                                      | :heavy_check_mark: | N/A                                 |
| `subjectSlug`        | _string_                                                                                      | :heavy_minus_sign: | N/A                                 |
| `subjectType`        | _string_                                                                                      | :heavy_check_mark: | N/A                                 |
