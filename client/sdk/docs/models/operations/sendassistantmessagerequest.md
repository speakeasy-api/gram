# SendAssistantMessageRequest

## Example Usage

```typescript
import { SendAssistantMessageRequest } from "@gram/client/models/operations/sendassistantmessage.js";

let value: SendAssistantMessageRequest = {
  sendMessageRequestBody: {
    assistantId: "0ada3fdc-e6d8-4498-be80-9b64a8cf08d2",
    message: "<value>",
  },
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `gramSession`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | Session header                                                                         |
| `gramProject`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | project header                                                                         |
| `sendMessageRequestBody`                                                               | [components.SendMessageRequestBody](../../models/components/sendmessagerequestbody.md) | :heavy_check_mark:                                                                     | N/A                                                                                    |