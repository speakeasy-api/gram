# McpEnvironmentEntry

Represents an environment variable configured for an MCP server.

## Example Usage

```typescript
import { McpEnvironmentEntry } from "@gram/client/models/components";

let value: McpEnvironmentEntry = {
  createdAt: new Date("2025-09-06T08:16:35.004Z"),
  id: "<id>",
  providedBy: "<value>",
  updatedAt: new Date("2025-05-05T14:21:37.175Z"),
  variableName: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the entry was created                                                                    |
| `headerDisplayName`                                                                           | *string*                                                                                      | :heavy_minus_sign:                                                                            | Custom display name for the variable in MCP headers                                           |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the environment entry                                                               |
| `providedBy`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | How the variable is provided: 'user', 'system', or 'none'                                     |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the entry was last updated                                                               |
| `variableName`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the environment variable                                                          |