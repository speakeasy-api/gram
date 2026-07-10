# SendInviteRequestBody

## Example Usage

```typescript
import { SendInviteRequestBody } from "@gram/client/models/components/sendinviterequestbody.js";

let value: SendInviteRequestBody = {
  email: "Ernesto.Cremin-Mills@hotmail.com",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `email`                           | *string*                          | :heavy_check_mark:                | Email address to invite.          |
| `roleId`                          | *string*                          | :heavy_minus_sign:                | Optional role ID for the invitee. |