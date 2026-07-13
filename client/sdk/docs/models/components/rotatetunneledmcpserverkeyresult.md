# RotateTunneledMcpServerKeyResult

Rotated tunneled MCP server plus the one-time replacement tunnel key

## Example Usage

```typescript
import { RotateTunneledMcpServerKeyResult } from "@gram/client/models/components/rotatetunneledmcpserverkeyresult.js";

let value: RotateTunneledMcpServerKeyResult = {
  server: {
    activeConnectionCount: 978526,
    activeConsumerSessionCount: 470211,
    connectionStatus: "never_connected",
    createdAt: new Date("2024-07-10T03:57:31.147Z"),
    id: "01a9c49d-a409-4207-aabb-6c3adcc5fddb",
    keyPrefix: "<value>",
    name: "<value>",
    projectId: "6cf3af9f-c70a-4b67-b1fa-ef9b3843522c",
    status: "revoked",
    updatedAt: new Date("2024-06-08T03:52:43.606Z"),
  },
  tunnelKey: "<value>",
};
```

## Fields

| Field       | Type                                                                         | Required           | Description                                             |
| ----------- | ---------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------- |
| `server`    | [components.TunneledMcpServer](../../models/components/tunneledmcpserver.md) | :heavy_check_mark: | A customer-hosted MCP server connected through a tunnel |
| `tunnelKey` | _string_                                                                     | :heavy_check_mark: | Plaintext tunnel key. Only returned after rotation.     |
