# ClaudeAgentUsage

## Example Usage

```typescript
import { ClaudeAgentUsage } from "@gram/client/models/components/claudeagentusage.js";

let value: ClaudeAgentUsage = {
  tools: [
    {
      inputSizeBytes: 64490,
      promptId: "<id>",
      resultSizeBytes: 481562,
      toolName: "<value>",
      toolUseId: "<id>",
    },
  ],
  turns: [],
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `tools`                                                                    | [components.ClaudeToolUsage](../../models/components/claudetoolusage.md)[] | :heavy_check_mark:                                                         | Per-tool Claude usage keyed by tool_use_id.                                |
| `turns`                                                                    | [components.ClaudeTurnUsage](../../models/components/claudeturnusage.md)[] | :heavy_check_mark:                                                         | Per-prompt Claude usage turns ordered by start time.                       |