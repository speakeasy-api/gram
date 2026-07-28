# RemoveOrganizationRemoteSessionClientFromMcpServerRequest

## Example Usage

```typescript
import { RemoveOrganizationRemoteSessionClientFromMcpServerRequest } from "@gram/client/models/operations/removeorganizationremotesessionclientfrommcpserver.js";

let value: RemoveOrganizationRemoteSessionClientFromMcpServerRequest = {
  removeClientFromMcpServerRequestBody: {
    clientId: "69e54d6f-4324-4f5e-9250-a960f0ec0e52",
    mcpServerId: "eb2f58eb-8557-4d60-aabd-085ceb2b307a",
  },
};
```

## Fields

| Field                                  | Type                                                                                                               | Required           | Description    |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramSession`                          | _string_                                                                                                           | :heavy_minus_sign: | Session header |
| `gramKey`                              | _string_                                                                                                           | :heavy_minus_sign: | API Key header |
| `removeClientFromMcpServerRequestBody` | [components.RemoveClientFromMcpServerRequestBody](../../models/components/removeclientfrommcpserverrequestbody.md) | :heavy_check_mark: | N/A            |
