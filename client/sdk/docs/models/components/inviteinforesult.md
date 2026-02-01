# InviteInfoResult

## Example Usage

```typescript
import { InviteInfoResult } from "@gram/client/models/components";

let value: InviteInfoResult = {
  email: "Bertrand.Legros70@hotmail.com",
  inviterName: "<value>",
  organizationName: "<value>",
  status: "pending",
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `email`                                                                                | *string*                                                                               | :heavy_check_mark:                                                                     | The email address the invite was sent to                                               |
| `inviterName`                                                                          | *string*                                                                               | :heavy_check_mark:                                                                     | Display name of the user who sent the invite                                           |
| `organizationName`                                                                     | *string*                                                                               | :heavy_check_mark:                                                                     | Name of the organization                                                               |
| `status`                                                                               | [components.InviteInfoResultStatus](../../models/components/inviteinforesultstatus.md) | :heavy_check_mark:                                                                     | Current status of the invite                                                           |