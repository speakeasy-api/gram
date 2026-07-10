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

| Field          | Type                                                                                          | Required           | Description                              |
| -------------- | --------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------- |
| `email`        | _string_                                                                                      | :heavy_check_mark: | Email address.                           |
| `id`           | _string_                                                                                      | :heavy_check_mark: | User ID.                                 |
| `joinedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the member joined the organization. |
| `name`         | _string_                                                                                      | :heavy_check_mark: | Display name.                            |
| `photoUrl`     | _string_                                                                                      | :heavy_minus_sign: | Avatar URL.                              |
| `principalUrn` | _string_                                                                                      | :heavy_check_mark: | Canonical principal URN for this member. |
| `roleIds`      | _string_[]                                                                                    | :heavy_check_mark: | All role IDs assigned to this member.    |
