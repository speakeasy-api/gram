# TeamInvite

## Example Usage

```typescript
import { TeamInvite } from "@gram/client/models/components";

let value: TeamInvite = {
  createdAt: new Date("2026-07-06T07:55:11.382Z"),
  email: "Gideon27@hotmail.com",
  expiresAt: new Date("2025-10-05T18:41:27.269Z"),
  id: "e962f53c-ade8-4735-ba69-49073f0fb805",
  invitedBy: "<value>",
  status: "pending",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the invite was created                                                                   |
| `email`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The invited email address                                                                     |
| `expiresAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the invite expires                                                                       |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The invite ID                                                                                 |
| `invitedBy`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | Name of the user who sent the invite                                                          |
| `status`                                                                                      | [components.TeamInviteStatus](../../models/components/teaminvitestatus.md)                    | :heavy_check_mark:                                                                            | N/A                                                                                           |