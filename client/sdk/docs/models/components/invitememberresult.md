# InviteMemberResult

## Example Usage

```typescript
import { InviteMemberResult } from "@gram/client/models/components";

let value: InviteMemberResult = {
  invite: {
    createdAt: new Date("2026-12-24T23:38:15.463Z"),
    email: "Gustave.Lemke@yahoo.com",
    expiresAt: new Date("2024-02-01T14:44:50.475Z"),
    id: "908e9d37-aa38-4df1-b2e1-6df8a0e63ff4",
    invitedBy: "<value>",
    status: "accepted",
  },
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `invite`                                                       | [components.TeamInvite](../../models/components/teaminvite.md) | :heavy_check_mark:                                             | N/A                                                            |