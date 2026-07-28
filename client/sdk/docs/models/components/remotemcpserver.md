# RemoteMcpServer

A remote MCP server configuration

## Example Usage

```typescript
import { RemoteMcpServer } from "@gram/client/models/components/remotemcpserver.js";

let value: RemoteMcpServer = {
  createdAt: new Date("2026-12-27T14:25:34.539Z"),
  headers: [],
  id: "ea0fcca2-5afd-49d0-9ae3-6663d99154d7",
  projectId: "287143c3-8e47-45ad-b8ec-59e45d69559b",
  transportType: "<value>",
  updatedAt: new Date("2026-11-04T00:56:37.042Z"),
  url: "https://sneaky-middle.name",
};
```

## Fields

| Field           | Type                                                                                          | Required           | Description                                            |
| --------------- | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------ |
| `createdAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the remote MCP server was created                 |
| `headers`       | [components.RemoteMcpServerHeader](../../models/components/remotemcpserverheader.md)[]        | :heavy_check_mark: | Headers configured for this remote MCP server          |
| `id`            | _string_                                                                                      | :heavy_check_mark: | The ID of the remote MCP server                        |
| `name`          | _string_                                                                                      | :heavy_minus_sign: | Optional human-readable name for the remote MCP server |
| `projectId`     | _string_                                                                                      | :heavy_check_mark: | The project ID this remote MCP server belongs to       |
| `slug`          | _string_                                                                                      | :heavy_minus_sign: | URL-friendly slug derived from the URL and ID.         |
| `transportType` | _string_                                                                                      | :heavy_check_mark: | The transport type for the remote MCP server           |
| `updatedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | When the remote MCP server was last updated            |
| `url`           | _string_                                                                                      | :heavy_check_mark: | The URL of the remote MCP server                       |
