# CreateSignedChatAttachmentURLRequest

## Example Usage

```typescript
import { CreateSignedChatAttachmentURLRequest } from "@gram/client/models/operations/createsignedchatattachmenturl.js";

let value: CreateSignedChatAttachmentURLRequest = {
  createSignedChatAttachmentURLForm2: {
    id: "<id>",
    projectId: "<id>",
  },
};
```

## Fields

| Field                                | Type                                                                                                           | Required           | Description                |
| ------------------------------------ | -------------------------------------------------------------------------------------------------------------- | ------------------ | -------------------------- |
| `gramKey`                            | _string_                                                                                                       | :heavy_minus_sign: | API Key header             |
| `gramProject`                        | _string_                                                                                                       | :heavy_minus_sign: | project header             |
| `gramSession`                        | _string_                                                                                                       | :heavy_minus_sign: | Session header             |
| `gramChatSession`                    | _string_                                                                                                       | :heavy_minus_sign: | Chat Sessions token header |
| `createSignedChatAttachmentURLForm2` | [components.CreateSignedChatAttachmentURLForm2](../../models/components/createsignedchatattachmenturlform2.md) | :heavy_check_mark: | N/A                        |
