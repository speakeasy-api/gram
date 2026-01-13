# ArchiveNotificationRequest

## Example Usage

```typescript
import { ArchiveNotificationRequest } from "@gram/client/models/operations";

let value: ArchiveNotificationRequest = {
  archiveNotificationRequestBody: {
    id: "f723515a-8e55-4be8-9e7f-38ef3e2e43a8",
  },
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | Session header                                                                                         |
| `gramProject`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | project header                                                                                         |
| `archiveNotificationRequestBody`                                                                       | [components.ArchiveNotificationRequestBody](../../models/components/archivenotificationrequestbody.md) | :heavy_check_mark:                                                                                     | N/A                                                                                                    |