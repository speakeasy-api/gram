# RevokeInviteRequest

## Example Usage

```typescript
import { RevokeInviteRequest } from "@gram/client/models/operations/revokeinvite.js";

let value: RevokeInviteRequest = {
  invitationId: "<id>",
};
```

## Fields

| Field          | Type     | Required           | Description           |
| -------------- | -------- | ------------------ | --------------------- |
| `invitationId` | _string_ | :heavy_check_mark: | WorkOS invitation ID. |
| `gramSession`  | _string_ | :heavy_minus_sign: | Session header        |
