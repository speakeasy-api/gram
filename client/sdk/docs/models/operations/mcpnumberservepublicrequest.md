# McpNumberServePublicRequest

## Example Usage

```typescript
import { McpNumberServePublicRequest } from "@gram/client/models/operations";

let value: McpNumberServePublicRequest = {
  projectID: "<id>",
  toolset: "<value>",
};
```

## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `projectID`                                                               | *string*                                                                  | :heavy_check_mark:                                                        | The project ID.                                                           |
| `toolset`                                                                 | *string*                                                                  | :heavy_check_mark:                                                        | The toolset to access via MCP.                                            |
| `mcpEnvironment`                                                          | *string*                                                                  | :heavy_minus_sign:                                                        | The environment variables passed by user to MCP server (JSON Structured). |