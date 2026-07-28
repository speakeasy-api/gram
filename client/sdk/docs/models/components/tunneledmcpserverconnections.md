# TunneledMcpServerConnections

Live connection details for a tunneled MCP server

## Example Usage

```typescript
import { TunneledMcpServerConnections } from "@gram/client/models/components/tunneledmcpserverconnections.js";

let value: TunneledMcpServerConnections = {
  activeConnectionCount: 462854,
  activeConsumerSessionCount: 422459,
  connections: [],
};
```

## Fields

| Field                        | Type                                                                                   | Required           | Description                                                                   |
| ---------------------------- | -------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------- |
| `activeConnectionCount`      | _number_                                                                               | :heavy_check_mark: | Number of active tunnel connections currently visible in Redis                |
| `activeConsumerSessionCount` | _number_                                                                               | :heavy_check_mark: | Total MCP consumer sessions currently pinned across active tunnel connections |
| `connections`                | [components.TunneledMcpConnection](../../models/components/tunneledmcpconnection.md)[] | :heavy_check_mark: | Live tunnel connections currently visible in Redis                            |
