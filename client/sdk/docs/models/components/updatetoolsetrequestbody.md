# UpdateToolsetRequestBody

## Example Usage

```typescript
import { UpdateToolsetRequestBody } from "@gram/client/models/components/updatetoolsetrequestbody.js";

let value: UpdateToolsetRequestBody = {};
```

## Fields

| Field                    | Type       | Required           | Description                                                                    |
| ------------------------ | ---------- | ------------------ | ------------------------------------------------------------------------------ |
| `customDomainId`         | _string_   | :heavy_minus_sign: | The ID of the custom domain to use for the toolset                             |
| `defaultEnvironmentSlug` | _string_   | :heavy_minus_sign: | The slug of the environment to use as the default for the toolset              |
| `description`            | _string_   | :heavy_minus_sign: | The new description of the toolset                                             |
| `mcpEnabled`             | _boolean_  | :heavy_minus_sign: | Whether the toolset is enabled for MCP                                         |
| `mcpIsPublic`            | _boolean_  | :heavy_minus_sign: | Whether the toolset is public in MCP                                           |
| `mcpSlug`                | _string_   | :heavy_minus_sign: | The slug of the MCP to use for the toolset                                     |
| `name`                   | _string_   | :heavy_minus_sign: | The new name of the toolset                                                    |
| `promptTemplateNames`    | _string_[] | :heavy_minus_sign: | List of prompt template names to include (note: for actual prompts, not tools) |
| `resourceUrns`           | _string_[] | :heavy_minus_sign: | List of resource URNs to include in the toolset                                |
| `toolSelectionMode`      | _string_   | :heavy_minus_sign: | The mode to use for tool selection                                             |
| `toolUrns`               | _string_[] | :heavy_minus_sign: | List of tool URNs to include in the toolset                                    |
