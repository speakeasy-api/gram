# OrganizationInvitation

## Example Usage

```typescript
import { OrganizationInvitation } from "@gram/client/models/components/organizationinvitation.js";

let value: OrganizationInvitation = {
  createdAt: new Date("2024-09-27T12:45:26.394Z"),
  email: "Devon.Jones@gmail.com",
  id: "<id>",
  state: "revoked",
  updatedAt: new Date("2025-04-20T00:04:59.775Z"),
};
```

## Fields

| Field           | Type                                                                                          | Required           | Description                                            |
| --------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------ |
| `acceptedAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | When the invitation was accepted.                      |
| `createdAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                                    |
| `email`         | _string_                                                                                      | :heavy_check_mark: | Invitee email address.                                 |
| `expiresAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | When the invitation expires.                           |
| `id`            | _string_                                                                                      | :heavy_check_mark: | WorkOS invitation ID.                                  |
| `inviterUserId` | _string_                                                                                      | :heavy_minus_sign: | Gram user ID of the inviter, when known.               |
| `revokedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | When the invitation was revoked.                       |
| `roleSlug`      | _string_                                                                                      | :heavy_minus_sign: | WorkOS role slug assigned when the invite is accepted. |
| `state`         | [components.State](../../models/components/state.md)                                          | :heavy_check_mark: | Invitation lifecycle state.                            |
| `updatedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                                    |
