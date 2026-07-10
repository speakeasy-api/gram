# OrganizationUser

## Example Usage

```typescript
import { OrganizationUser } from "@gram/client/models/components/organizationuser.js";

let value: OrganizationUser = {
  createdAt: new Date("2026-07-25T18:31:45.779Z"),
  email: "Cesar_White@gmail.com",
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  updatedAt: new Date("2025-07-17T21:43:44.748Z"),
  userId: "<id>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `email`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | User email address.                                                                           |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Gram relationship row ID.                                                                     |
| `lastLogin`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Timestamp of the user's most recent login.                                                    |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | User display name.                                                                            |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | Gram organization ID.                                                                         |
| `photoUrl`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | User photo URL.                                                                               |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `userId`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | Gram user ID.                                                                                 |
| `workosMembershipId`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | WorkOS organization membership ID when known.                                                 |