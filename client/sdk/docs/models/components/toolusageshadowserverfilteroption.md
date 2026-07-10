# ToolUsageShadowServerFilterOption

Shadow MCP server filter option with usage in the selected time window

## Example Usage

```typescript
import { ToolUsageShadowServerFilterOption } from "@gram/client/models/components/toolusageshadowserverfilteroption.js";

let value: ToolUsageShadowServerFilterOption = {
  eventCount: 715788,
  serverName: "<value>",
};
```

## Fields

| Field        | Type     | Required           | Description                                                    |
| ------------ | -------- | ------------------ | -------------------------------------------------------------- |
| `eventCount` | _number_ | :heavy_check_mark: | Number of tool usage events observed for the Shadow MCP server |
| `serverName` | _string_ | :heavy_check_mark: | Observed Shadow MCP server name                                |
