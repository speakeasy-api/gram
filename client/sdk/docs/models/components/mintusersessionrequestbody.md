# MintUserSessionRequestBody

## Example Usage

```typescript
import { MintUserSessionRequestBody } from "@gram/client/models/components/mintusersessionrequestbody.js";

let value: MintUserSessionRequestBody = {};
```

## Fields

| Field         | Type     | Required           | Description                                                                                                                                                                                                                                              |
| ------------- | -------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `mcpServerId` | _string_ | :heavy_minus_sign: | Bind the JWT to this remote MCP server's user_session_issuer audience (the /x/mcp convention, since remote servers have no toolset). Mutually exclusive with toolset_id; exactly one must be set. Must be issuer-gated and live in the caller's project. |
| `toolsetId`   | _string_ | :heavy_minus_sign: | Bind the JWT to this toolset's /mcp/{slug} audience. Mutually exclusive with mcp_server_id; exactly one must be set. Must be issuer-gated and live in the caller's project.                                                                              |
