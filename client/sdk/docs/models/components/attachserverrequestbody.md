# AttachServerRequestBody

## Example Usage

```typescript
import { AttachServerRequestBody } from "@gram/client/models/components/attachserverrequestbody.js";

let value: AttachServerRequestBody = {
  collectionId: "1c35cdaf-ae32-4b11-8b61-5aa6011d4ec2",
};
```

## Fields

| Field          | Type     | Required           | Description                                                                         |
| -------------- | -------- | ------------------ | ----------------------------------------------------------------------------------- |
| `collectionId` | _string_ | :heavy_check_mark: | ID of the collection                                                                |
| `mcpServerId`  | _string_ | :heavy_minus_sign: | ID of the MCP server to attach (provide exactly one of toolset_id or mcp_server_id) |
| `toolsetId`    | _string_ | :heavy_minus_sign: | ID of the toolset to attach (provide exactly one of toolset_id or mcp_server_id)    |
