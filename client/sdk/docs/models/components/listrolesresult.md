# ListRolesResult

## Example Usage

```typescript
import { ListRolesResult } from "@gram/client/models/components/listrolesresult.js";

let value: ListRolesResult = {
  roles: [
    {
      createdAt: new Date("2024-04-19T20:44:13.988Z"),
      description: "among kindly for lazily equally bogus",
      grants: [
        {
          scope: "environment:read",
        },
      ],
      id: "<id>",
      isSystem: false,
      memberCount: 705643,
      name: "<value>",
      principalUrn: "<value>",
      slug: "<value>",
      updatedAt: new Date("2025-01-25T11:00:19.815Z"),
    },
  ],
};
```

## Fields

| Field   | Type                                                 | Required           | Description                     |
| ------- | ---------------------------------------------------- | ------------------ | ------------------------------- |
| `roles` | [components.Role](../../models/components/role.md)[] | :heavy_check_mark: | The roles in your organization. |
