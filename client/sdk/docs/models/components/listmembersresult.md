# ListMembersResult

## Example Usage

```typescript
import { ListMembersResult } from "@gram/client/models/components/listmembersresult.js";

let value: ListMembersResult = {
  members: [
    {
      email: "Alta.Lindgren@hotmail.com",
      id: "<id>",
      joinedAt: new Date("2025-02-14T23:30:54.184Z"),
      name: "<value>",
      principalUrn: "<value>",
      roleIds: [
        "<value 1>",
        "<value 2>",
      ],
    },
  ],
};
```

## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `members`                                                            | [components.AccessMember](../../models/components/accessmember.md)[] | :heavy_check_mark:                                                   | The members in your organization.                                    |