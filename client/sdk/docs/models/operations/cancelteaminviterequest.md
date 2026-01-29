# CancelTeamInviteRequest

## Example Usage

```typescript
import { CancelTeamInviteRequest } from "@gram/client/models/operations";

let value: CancelTeamInviteRequest = {
  inviteId: "fc7a012a-ab9c-4f4b-9b28-439f5a446580",
};
```

## Fields

| Field                          | Type                           | Required                       | Description                    |
| ------------------------------ | ------------------------------ | ------------------------------ | ------------------------------ |
| `inviteId`                     | *string*                       | :heavy_check_mark:             | The ID of the invite to cancel |
| `gramSession`                  | *string*                       | :heavy_minus_sign:             | Session header                 |