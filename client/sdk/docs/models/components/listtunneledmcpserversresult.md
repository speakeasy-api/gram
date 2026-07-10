# ListTunneledMcpServersResult

Result type for listing tunneled MCP servers

## Example Usage

```typescript
import { ListTunneledMcpServersResult } from "@gram/client/models/components/listtunneledmcpserversresult.js";

let value: ListTunneledMcpServersResult = {
  tunneledMcpServers: [
    {
      activeConnectionCount: 540947,
      activeConsumerSessionCount: 745212,
      connectionStatus: "connected",
      createdAt: new Date("2024-09-12T09:35:19.814Z"),
      id: "b8a73612-7f04-4cc3-b546-65b1182f05bc",
      keyPrefix: "<value>",
      name: "<value>",
      projectId: "550b13b4-4084-4d1e-8ad5-de216410d7a9",
      status: "active",
      updatedAt: new Date("2024-11-27T09:44:32.920Z"),
    },
  ],
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `tunneledMcpServers`                                                           | [components.TunneledMcpServer](../../models/components/tunneledmcpserver.md)[] | :heavy_check_mark:                                                             | N/A                                                                            |