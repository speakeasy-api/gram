# AcceptTeamInviteRequest

## Example Usage

```typescript
import { AcceptTeamInviteRequest } from "@gram/client/models/operations";

let value: AcceptTeamInviteRequest = {
  serveChatAttachmentSignedForm: {
    token: "<value>",
  },
};
```

## Fields

| Field                                                                                                | Type                                                                                                 | Required                                                                                             | Description                                                                                          |
| ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                        | *string*                                                                                             | :heavy_minus_sign:                                                                                   | Session header                                                                                       |
| `serveChatAttachmentSignedForm`                                                                      | [components.ServeChatAttachmentSignedForm](../../models/components/servechatattachmentsignedform.md) | :heavy_check_mark:                                                                                   | N/A                                                                                                  |