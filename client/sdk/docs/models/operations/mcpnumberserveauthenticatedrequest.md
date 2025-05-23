# McpNumberServeAuthenticatedRequest

## Example Usage

```typescript
import { McpNumberServeAuthenticatedRequest } from "@gram/client/models/operations";

let value: McpNumberServeAuthenticatedRequest = {
  project: "<value>",
  toolset: "<value>",
  environment: "<value>",
};
```

## Fields

| Field                                                                     | Type                                                                      | Required                                                                  | Description                                                               |
| ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- | ------------------------------------------------------------------------- |
| `project`                                                                 | *string*                                                                  | :heavy_check_mark:                                                        | N/A                                                                       |
| `toolset`                                                                 | *string*                                                                  | :heavy_check_mark:                                                        | The toolset to access via MCP.                                            |
| `environment`                                                             | *string*                                                                  | :heavy_check_mark:                                                        | The environment to access via MCP.                                        |
| `mcpEnvironment`                                                          | *string*                                                                  | :heavy_minus_sign:                                                        | The environment variables passed by user to MCP server (JSON Structured). |