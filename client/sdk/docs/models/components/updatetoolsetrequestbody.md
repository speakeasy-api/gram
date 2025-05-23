# UpdateToolsetRequestBody

## Example Usage

```typescript
import { UpdateToolsetRequestBody } from "@gram/client/models/components";

let value: UpdateToolsetRequestBody = {};
```

## Fields

| Field                                                             | Type                                                              | Required                                                          | Description                                                       |
| ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- | ----------------------------------------------------------------- |
| `defaultEnvironmentSlug`                                          | *string*                                                          | :heavy_minus_sign:                                                | The slug of the environment to use as the default for the toolset |
| `description`                                                     | *string*                                                          | :heavy_minus_sign:                                                | The new description of the toolset                                |
| `httpToolNames`                                                   | *string*[]                                                        | :heavy_minus_sign:                                                | List of HTTP tool names to include                                |
| `mcpIsPublic`                                                     | *boolean*                                                         | :heavy_minus_sign:                                                | Whether the toolset is public in MCP                              |
| `mcpSlug`                                                         | *string*                                                          | :heavy_minus_sign:                                                | The slug of the MCP to use for the toolset                        |
| `name`                                                            | *string*                                                          | :heavy_minus_sign:                                                | The new name of the toolset                                       |