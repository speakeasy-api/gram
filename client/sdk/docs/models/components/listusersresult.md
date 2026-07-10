# ListUsersResult

## Example Usage

```typescript
import { ListUsersResult } from "@gram/client/models/components/listusersresult.js";

let value: ListUsersResult = {
  users: [
    {
      createdAt: new Date("2026-07-26T10:03:44.238Z"),
      email: "Greta5@gmail.com",
      id: "<id>",
      name: "<value>",
      organizationId: "<id>",
      updatedAt: new Date("2024-01-24T15:10:55.404Z"),
      userId: "<id>",
    },
  ],
};
```

## Fields

| Field   | Type                                                                         | Required           | Description                               |
| ------- | ---------------------------------------------------------------------------- | ------------------ | ----------------------------------------- |
| `users` | [components.OrganizationUser](../../models/components/organizationuser.md)[] | :heavy_check_mark: | Users linked to the organization in Gram. |
