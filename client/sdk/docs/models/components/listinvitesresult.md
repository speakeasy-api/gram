# ListInvitesResult

## Example Usage

```typescript
import { ListInvitesResult } from "@gram/client/models/components/listinvitesresult.js";

let value: ListInvitesResult = {
  invitations: [
    {
      createdAt: new Date("2025-01-21T17:50:53.678Z"),
      email: "Amina22@hotmail.com",
      id: "<id>",
      state: "pending",
      updatedAt: new Date("2025-12-25T02:42:51.654Z"),
    },
  ],
};
```

## Fields

| Field         | Type                                                                                     | Required           | Description                                                                                            |
| ------------- | ---------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------ |
| `invitations` | [components.OrganizationInvitation](../../models/components/organizationinvitation.md)[] | :heavy_check_mark: | Pending invitations for the organization only; accepted, expired, and revoked invitations are omitted. |
