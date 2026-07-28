# TunneledMcpServer

A customer-hosted MCP server connected through a tunnel

## Example Usage

```typescript
import { TunneledMcpServer } from "@gram/client/models/components/tunneledmcpserver.js";

let value: TunneledMcpServer = {
  activeConnectionCount: 607140,
  activeConsumerSessionCount: 351395,
  connectionStatus: "inactive",
  createdAt: new Date("2026-10-31T17:18:07.268Z"),
  id: "11b54dc1-7101-4dfe-8230-4d13f261ae6c",
  keyPrefix: "<value>",
  name: "<value>",
  projectId: "eacf65ce-b8be-43d8-8883-75f99165ab2c",
  status: "active",
  updatedAt: new Date("2026-12-07T06:49:34.056Z"),
};
```

## Fields

| Field                        | Type                                                                                          | Required           | Description                                                                   |
| ---------------------------- | --------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------------------------------------------- |
| `activeConnectionCount`      | _number_                                                                                      | :heavy_check_mark: | Number of active tunnel connections currently visible in Redis                |
| `activeConsumerSessionCount` | _number_                                                                                      | :heavy_check_mark: | Total MCP consumer sessions currently pinned across active tunnel connections |
| `agentVersion`               | _string_                                                                                      | :heavy_minus_sign: | Most recent agent version reported by the tunnel                              |
| `connectionStatus`           | [components.ConnectionStatus](../../models/components/connectionstatus.md)                    | :heavy_check_mark: | Derived live connection status for a tunneled MCP server source               |
| `createdAt`                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the tunneled MCP server source was created                               |
| `id`                         | _string_                                                                                      | :heavy_check_mark: | The ID of the tunneled MCP server                                             |
| `keyPrefix`                  | _string_                                                                                      | :heavy_check_mark: | Non-secret prefix of the tunnel key                                           |
| `lastSeenAt`                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Most recent persisted heartbeat timestamp                                     |
| `name`                       | _string_                                                                                      | :heavy_check_mark: | Human-readable name for the tunneled MCP server                               |
| `projectId`                  | _string_                                                                                      | :heavy_check_mark: | The project ID this tunneled MCP server belongs to                            |
| `status`                     | [components.TunneledMcpServerStatus](../../models/components/tunneledmcpserverstatus.md)      | :heavy_check_mark: | Stored lifecycle status for a tunneled MCP server source                      |
| `updatedAt`                  | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the tunneled MCP server source was last updated                          |
