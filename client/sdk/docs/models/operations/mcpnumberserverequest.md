# McpNumberServeRequest

## Example Usage

```typescript
import { McpNumberServeRequest } from "@gram/client/models/operations";

let value: McpNumberServeRequest = {
  project: "<value>",
  toolset: "<value>",
  environment: "<value>",
};
```

## Fields

| Field                              | Type                               | Required                           | Description                        |
| ---------------------------------- | ---------------------------------- | ---------------------------------- | ---------------------------------- |
| `project`                          | *string*                           | :heavy_check_mark:                 | N/A                                |
| `toolset`                          | *string*                           | :heavy_check_mark:                 | The toolset to access via MCP.     |
| `environment`                      | *string*                           | :heavy_check_mark:                 | The environment to access via MCP. |