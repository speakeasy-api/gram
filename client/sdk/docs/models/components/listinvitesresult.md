# ListInvitesResult

## Example Usage

```typescript
import { ListInvitesResult } from "@gram/client/models/components";

let value: ListInvitesResult = {
  invites: [
    {
      createdAt: new Date("2025-01-21T17:50:53.678Z"),
      email: "Amina22@hotmail.com",
      expiresAt: new Date("2024-07-30T13:40:38.351Z"),
      id: "a0a36759-279c-43ef-ade9-9a67bdff5569",
      invitedBy: "<value>",
      status: "accepted",
    },
  ],
};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `invites`                                                        | [components.TeamInvite](../../models/components/teaminvite.md)[] | :heavy_check_mark:                                               | List of pending invites                                          |