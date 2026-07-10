# TunneledMcpConnection

## Example Usage

```typescript
import { TunneledMcpConnection } from "@gram/client/models/components/tunneledmcpconnection.js";

let value: TunneledMcpConnection = {
  activeConsumerSessions: 934259,
  activeSubstreams: 335255,
  connectedAt: new Date("2025-03-02T11:00:10.871Z"),
  gatewaySessionId: "<id>",
  lastHeartbeatAt: new Date("2025-03-26T03:04:04.854Z"),
  metadata: {},
  serviceVersion: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `activeConsumerSessions`                                                                      | *number*                                                                                      | :heavy_check_mark:                                                                            | Number of MCP consumer sessions currently pinned to this tunnel connection                    |
| `activeSubstreams`                                                                            | *number*                                                                                      | :heavy_check_mark:                                                                            | Number of active request substreams on this tunnel session                                    |
| `agentVersion`                                                                                | *string*                                                                                      | :heavy_minus_sign:                                                                            | Tunnel agent version reported by the connection                                               |
| `connectedAt`                                                                                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When this tunnel session connected                                                            |
| `gatewaySessionId`                                                                            | *string*                                                                                      | :heavy_check_mark:                                                                            | Gateway session ID for a live tunnel connection                                               |
| `lastHeartbeatAt`                                                                             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Most recent heartbeat observed for this tunnel session                                        |
| `metadata`                                                                                    | Record<string, *string*>                                                                      | :heavy_check_mark:                                                                            | User-provided tunnel metadata reported by the agent                                           |
| `remoteAddr`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Remote address reported by the gateway                                                        |
| `serviceVersion`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | Customer-declared version of the MCP service behind this tunnel connection                    |