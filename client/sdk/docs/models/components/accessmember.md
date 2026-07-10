# AccessMember

## Example Usage

```typescript
import { AccessMember } from "@gram/client/models/components/accessmember.js";

let value: AccessMember = {
  email: "Bradford4@yahoo.com",
  id: "<id>",
  joinedAt: new Date("2025-10-18T15:53:10.747Z"),
  name: "<value>",
  principalUrn: "<value>",
  roleIds: [],
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `email`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | Email address.                                                                                |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | User ID.                                                                                      |
| `joinedAt`                                                                                    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the member joined the organization.                                                      |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Display name.                                                                                 |
| `photoUrl`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | Avatar URL.                                                                                   |
| `principalUrn`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | Canonical principal URN for this member.                                                      |
| `roleIds`                                                                                     | *string*[]                                                                                    | :heavy_check_mark:                                                                            | All role IDs assigned to this member.                                                         |