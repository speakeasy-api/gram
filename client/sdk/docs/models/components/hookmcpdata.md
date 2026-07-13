# HookMCPData

MCP feature payload.

## Example Usage

```typescript
import { HookMCPData } from "@gram/client/models/components/hookmcpdata.js";

let value: HookMCPData = {};
```

## Fields

| Field            | Type     | Required           | Description                                              |
| ---------------- | -------- | ------------------ | -------------------------------------------------------- |
| `command`        | _string_ | :heavy_minus_sign: | MCP server command, when available.                      |
| `resultJson`     | _string_ | :heavy_minus_sign: | JSON-encoded MCP tool result, when reported as a string. |
| `serverIdentity` | _string_ | :heavy_minus_sign: | Stable server identity inferred by the hook adapter.     |
| `serverName`     | _string_ | :heavy_minus_sign: | Provider-reported MCP server name.                       |
| `url`            | _string_ | :heavy_minus_sign: | MCP server URL, when available.                          |
