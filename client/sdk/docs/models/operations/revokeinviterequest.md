# RevokeInviteRequest

## Example Usage

```typescript
import { RevokeInviteRequest } from "@gram/client/models/operations/revokeinvite.js";

let value: RevokeInviteRequest = {
  invitationId: "<id>",
};
```

## Fields

| Field                 | Type                  | Required              | Description           |
| --------------------- | --------------------- | --------------------- | --------------------- |
| `invitationId`        | *string*              | :heavy_check_mark:    | WorkOS invitation ID. |
| `gramSession`         | *string*              | :heavy_minus_sign:    | Session header        |