# ListMembersResult

## Example Usage

```typescript
import { ListMembersResult } from "@gram/client/models/components";

let value: ListMembersResult = {
  members: [
    {
      displayName: "Whitney20",
      email: "Garnett_Mayer98@yahoo.com",
      id: "<id>",
      joinedAt: new Date("2024-08-22T05:36:15.734Z"),
    },
  ],
};
```

## Fields

| Field                                                            | Type                                                             | Required                                                         | Description                                                      |
| ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- | ---------------------------------------------------------------- |
| `members`                                                        | [components.TeamMember](../../models/components/teammember.md)[] | :heavy_check_mark:                                               | List of organization members                                     |