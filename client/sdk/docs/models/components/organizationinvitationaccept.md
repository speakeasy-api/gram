# OrganizationInvitationAccept

## Example Usage

```typescript
import { OrganizationInvitationAccept } from "@gram/client/models/components";

let value: OrganizationInvitationAccept = {
  acceptInvitationUrl: "https://alert-brush.com/",
  email: "Agustin_Krajcik94@hotmail.com",
  organizationName: "<value>",
  state: "accepted",
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `acceptInvitationUrl`                                                            | *string*                                                                         | :heavy_check_mark:                                                               | URL to complete acceptance in WorkOS (may be empty when not actionable).         |
| `email`                                                                          | *string*                                                                         | :heavy_check_mark:                                                               | Invitee email address.                                                           |
| `organizationName`                                                               | *string*                                                                         | :heavy_check_mark:                                                               | Gram organization display name when the org is linked in Gram; empty if unknown. |
| `state`                                                                          | [components.State](../../models/components/state.md)                             | :heavy_check_mark:                                                               | Invitation lifecycle state.                                                      |