# UpdateInviteRoleRequest

## Example Usage

```typescript
import { UpdateInviteRoleRequest } from "@gram/client/models/operations/updateinviterole.js";

let value: UpdateInviteRoleRequest = {
  updateInviteRoleRequestBody: {
    invitationId: "<id>",
    roleId: "<id>",
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |
| `updateInviteRoleRequestBody`                                                                    | [components.UpdateInviteRoleRequestBody](../../models/components/updateinviterolerequestbody.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |