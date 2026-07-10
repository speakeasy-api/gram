# ExternalMCPRemote

A remote endpoint for an MCP server

## Example Usage

```typescript
import { ExternalMCPRemote } from "@gram/client/models/components/externalmcpremote.js";

let value: ExternalMCPRemote = {
  transportType: "sse",
  url: "https://potable-spirit.com",
};
```

## Fields

| Field           | Type                                                                                                         | Required           | Description                                                                        |
| --------------- | ------------------------------------------------------------------------------------------------------------ | ------------------ | ---------------------------------------------------------------------------------- |
| `headers`       | [components.ExternalMCPRemoteHeader](../../models/components/externalmcpremoteheader.md)[]                   | :heavy_minus_sign: | HTTP headers the MCP client should collect and send when connecting to this remote |
| `transportType` | [components.TransportType](../../models/components/transporttype.md)                                         | :heavy_check_mark: | Transport type (sse or streamable-http)                                            |
| `url`           | _string_                                                                                                     | :heavy_check_mark: | URL of the remote endpoint                                                         |
| `variables`     | Record<string, [components.ExternalMCPRemoteVariable](../../models/components/externalmcpremotevariable.md)> | :heavy_minus_sign: | URL template variables for this remote endpoint                                    |
