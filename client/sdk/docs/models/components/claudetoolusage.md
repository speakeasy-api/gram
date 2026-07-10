# ClaudeToolUsage

## Example Usage

```typescript
import { ClaudeToolUsage } from "@gram/client/models/components/claudetoolusage.js";

let value: ClaudeToolUsage = {
  inputSizeBytes: 549779,
  promptId: "<id>",
  resultSizeBytes: 402677,
  toolName: "<value>",
  toolUseId: "<id>",
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `inputSizeBytes`                                             | *number*                                                     | :heavy_check_mark:                                           | Serialized tool input size in bytes.                         |
| `promptId`                                                   | *string*                                                     | :heavy_check_mark:                                           | Claude prompt.id for the turn that used this tool.           |
| `resultSizeBytes`                                            | *number*                                                     | :heavy_check_mark:                                           | Serialized tool result size in bytes.                        |
| `toolName`                                                   | *string*                                                     | :heavy_check_mark:                                           | Tool name reported by Claude Code.                           |
| `toolUseId`                                                  | *string*                                                     | :heavy_check_mark:                                           | Claude tool_use_id that correlates the tool call and result. |