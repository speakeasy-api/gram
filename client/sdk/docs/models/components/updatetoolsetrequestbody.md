# UpdateToolsetRequestBody

## Example Usage

```typescript
import { UpdateToolsetRequestBody } from "@gram/client/models/components";

let value: UpdateToolsetRequestBody = {};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `customDomainId`                                                               | *string*                                                                       | :heavy_minus_sign:                                                             | The ID of the custom domain to use for the toolset                             |
| `defaultEnvironmentSlug`                                                       | *string*                                                                       | :heavy_minus_sign:                                                             | The slug of the environment to use as the default for the toolset              |
| `description`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | The new description of the toolset                                             |
| `mcpEnabled`                                                                   | *boolean*                                                                      | :heavy_minus_sign:                                                             | Whether the toolset is enabled for MCP                                         |
| `mcpIsPublic`                                                                  | *boolean*                                                                      | :heavy_minus_sign:                                                             | Whether the toolset is public in MCP                                           |
| `mcpSlug`                                                                      | *string*                                                                       | :heavy_minus_sign:                                                             | The slug of the MCP to use for the toolset                                     |
| `name`                                                                         | *string*                                                                       | :heavy_minus_sign:                                                             | The new name of the toolset                                                    |
| `promptTemplateNames`                                                          | *string*[]                                                                     | :heavy_minus_sign:                                                             | List of prompt template names to include (note: for actual prompts, not tools) |
| `resourceUrns`                                                                 | *string*[]                                                                     | :heavy_minus_sign:                                                             | List of resource URNs to include in the toolset                                |
| `toolSelectionMode`                                                            | *string*                                                                       | :heavy_minus_sign:                                                             | The mode to use for tool selection                                             |
| `toolUrns`                                                                     | *string*[]                                                                     | :heavy_minus_sign:                                                             | List of tool URNs to include in the toolset                                    |