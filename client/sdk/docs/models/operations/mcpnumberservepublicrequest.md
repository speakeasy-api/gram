# McpNumberServePublicRequest

## Example Usage

```typescript
import { McpNumberServePublicRequest } from "@gram/client/models/operations";

let value: McpNumberServePublicRequest = {
  mcpSlug: "<value>",
};
```

## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `mcpSlug`                                                                 | *string*                                                                  | :heavy_check_mark:                                                        | The unique slug of the mcp server.                                        |
| `mcpEnvironment`                                                          | *string*                                                                  | :heavy_minus_sign:                                                        | The environment variables passed by user to MCP server (JSON Structured). |