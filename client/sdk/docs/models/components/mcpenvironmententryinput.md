# McpEnvironmentEntryInput

Input for configuring an environment variable for an MCP server.

## Example Usage

```typescript
import { McpEnvironmentEntryInput } from "@gram/client/models/components";

let value: McpEnvironmentEntryInput = {
  providedBy: "<value>",
  variableName: "<value>",
};
```

## Fields

| Field                                                     | Type                                                      | Required                                                  | Description                                               |
| --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- |
| `headerDisplayName`                                       | *string*                                                  | :heavy_minus_sign:                                        | Custom display name for the variable in MCP headers       |
| `providedBy`                                              | *string*                                                  | :heavy_check_mark:                                        | How the variable is provided: 'user', 'system', or 'none' |
| `variableName`                                            | *string*                                                  | :heavy_check_mark:                                        | The name of the environment variable                      |