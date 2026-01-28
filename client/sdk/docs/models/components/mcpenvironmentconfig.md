# McpEnvironmentConfig

Represents an environment variable configured for an MCP server.

## Example Usage

```typescript
import { McpEnvironmentConfig } from "@gram/client/models/components";

let value: McpEnvironmentConfig = {
  createdAt: new Date("2025-01-20T05:07:46.082Z"),
  id: "<id>",
  providedBy: "<value>",
  updatedAt: new Date("2024-05-19T01:38:49.507Z"),
  variableName: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the config was created                                                                   |
| `headerDisplayName`                                                                           | *string*                                                                                      | :heavy_minus_sign:                                                                            | Custom display name for the variable in MCP headers                                           |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the environment config                                                              |
| `providedBy`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | How the variable is provided: 'user', 'system', or 'none'                                     |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the config was last updated                                                              |
| `variableName`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the environment variable                                                          |