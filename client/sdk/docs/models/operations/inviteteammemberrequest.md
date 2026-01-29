# InviteTeamMemberRequest

## Example Usage

```typescript
import { InviteTeamMemberRequest } from "@gram/client/models/operations";

let value: InviteTeamMemberRequest = {
  inviteMemberForm: {
    email: "Evangeline_Toy70@hotmail.com",
    organizationId: "<id>",
  },
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `gramSession`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | Session header                                                             |
| `inviteMemberForm`                                                         | [components.InviteMemberForm](../../models/components/invitememberform.md) | :heavy_check_mark:                                                         | N/A                                                                        |