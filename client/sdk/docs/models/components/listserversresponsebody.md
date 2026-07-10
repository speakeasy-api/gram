# ListServersResponseBody

## Example Usage

```typescript
import { ListServersResponseBody } from "@gram/client/models/components/listserversresponsebody.js";

let value: ListServersResponseBody = {
  servers: [],
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `servers`                                                                      | [components.ExternalMCPServer](../../models/components/externalmcpserver.md)[] | :heavy_check_mark:                                                             | List of available MCP servers                                                  |