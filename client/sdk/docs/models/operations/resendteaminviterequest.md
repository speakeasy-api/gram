# ResendTeamInviteRequest

## Example Usage

```typescript
import { ResendTeamInviteRequest } from "@gram/client/models/operations";

let value: ResendTeamInviteRequest = {
  resendInviteRequestBody: {
    inviteId: "ea15c1ed-5747-4c82-8372-9bbbfb7785f1",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `resendInviteRequestBody`                                                                | [components.ResendInviteRequestBody](../../models/components/resendinviterequestbody.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |