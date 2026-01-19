# CreateSignedChatAttachmentURLRequest

## Example Usage

```typescript
import { CreateSignedChatAttachmentURLRequest } from "@gram/client/models/operations";

let value: CreateSignedChatAttachmentURLRequest = {
  createSignedChatAttachmentURLForm2: {
    id: "<id>",
    projectId: "<id>",
  },
};
```

## Fields

| Field                                                                                                          | Type                                                                                                           | Required                                                                                                       | Description                                                                                                    |
| -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------- |
| `gramKey`                                                                                                      | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | API Key header                                                                                                 |
| `gramProject`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | project header                                                                                                 |
| `gramSession`                                                                                                  | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Session header                                                                                                 |
| `gramChatSession`                                                                                              | *string*                                                                                                       | :heavy_minus_sign:                                                                                             | Chat Sessions token header                                                                                     |
| `createSignedChatAttachmentURLForm2`                                                                           | [components.CreateSignedChatAttachmentURLForm2](../../models/components/createsignedchatattachmenturlform2.md) | :heavy_check_mark:                                                                                             | N/A                                                                                                            |