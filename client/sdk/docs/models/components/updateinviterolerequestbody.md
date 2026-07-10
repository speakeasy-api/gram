# UpdateInviteRoleRequestBody

## Example Usage

```typescript
import { UpdateInviteRoleRequestBody } from "@gram/client/models/components/updateinviterolerequestbody.js";

let value: UpdateInviteRoleRequestBody = {
  invitationId: "<id>",
  roleId: "<id>",
};
```

## Fields

| Field          | Type     | Required           | Description                       |
| -------------- | -------- | ------------------ | --------------------------------- |
| `invitationId` | _string_ | :heavy_check_mark: | WorkOS invitation ID.             |
| `roleId`       | _string_ | :heavy_check_mark: | Role ID to assign to the invitee. |
