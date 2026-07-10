# ToolUsageHostedServerFilterOption

Hosted MCP server filter option with usage in the selected time window

## Example Usage

```typescript
import { ToolUsageHostedServerFilterOption } from "@gram/client/models/components/toolusagehostedserverfilteroption.js";

let value: ToolUsageHostedServerFilterOption = {
  eventCount: 454130,
  toolsetName: "<value>",
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `eventCount`                                                   | *number*                                                       | :heavy_check_mark:                                             | Number of tool usage events observed for the hosted MCP server |
| `toolsetName`                                                  | *string*                                                       | :heavy_check_mark:                                             | Hosted MCP toolset display name                                |
| `toolsetSlug`                                                  | *string*                                                       | :heavy_check_mark:                                             | Hosted MCP toolset slug                                        |