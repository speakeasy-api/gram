# RemoveClientFromMcpServerRequestBody

## Example Usage

```typescript
import { RemoveClientFromMcpServerRequestBody } from "@gram/client/models/components/removeclientfrommcpserverrequestbody.js";

let value: RemoveClientFromMcpServerRequestBody = {
  clientId: "81b1c993-b0c2-4219-96b6-aee4d6656546",
  mcpServerId: "3e876877-cef9-4309-8dd1-7518089caa5a",
};
```

## Fields

| Field         | Type     | Required           | Description                       |
| ------------- | -------- | ------------------ | --------------------------------- |
| `clientId`    | _string_ | :heavy_check_mark: | The remote_session_client id.     |
| `mcpServerId` | _string_ | :heavy_check_mark: | The mcp_server id to detach from. |
