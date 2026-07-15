# GetToolUsageFilterOptionsResult

Filter options for target-aware MCP and tool usage metrics

## Example Usage

```typescript
import { GetToolUsageFilterOptionsResult } from "@gram/client/models/components/gettoolusagefilteroptionsresult.js";

let value: GetToolUsageFilterOptionsResult = {
  hostedServers: [
    {
      eventCount: 952106,
      toolsetName: "<value>",
      toolsetSlug: "<value>",
    },
  ],
  shadowServers: [],
  users: [
    {
      eventCount: 652935,
      userKey: "<value>",
      userKind: "unknown",
      userLabel: "<value>",
    },
  ],
};
```

## Fields

| Field           | Type                                                                                                           | Required           | Description                                              |
| --------------- | -------------------------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------- |
| `hostedServers` | [components.ToolUsageHostedServerFilterOption](../../models/components/toolusagehostedserverfilteroption.md)[] | :heavy_check_mark: | Hosted MCP servers with usage in the selected time range |
| `shadowServers` | [components.ToolUsageShadowServerFilterOption](../../models/components/toolusageshadowserverfilteroption.md)[] | :heavy_check_mark: | Shadow MCP servers with usage in the selected time range |
| `users`         | [components.ToolUsageUserFilterOption](../../models/components/toolusageuserfilteroption.md)[]                 | :heavy_check_mark: | User identities with usage in the selected time range    |
