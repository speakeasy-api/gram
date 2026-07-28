# SendInviteRequest

## Example Usage

```typescript
import { SendInviteRequest } from "@gram/client/models/operations/sendinvite.js";

let value: SendInviteRequest = {
  sendInviteRequestBody: {
    email: "Jarret_Lueilwitz32@yahoo.com",
  },
};
```

## Fields

| Field                   | Type                                                                                 | Required           | Description    |
| ----------------------- | ------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`           | _string_                                                                             | :heavy_minus_sign: | Session header |
| `sendInviteRequestBody` | [components.SendInviteRequestBody](../../models/components/sendinviterequestbody.md) | :heavy_check_mark: | N/A            |
