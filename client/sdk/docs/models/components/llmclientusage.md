# LLMClientUsage

Usage breakdown by LLM client/agent

## Example Usage

```typescript
import { LLMClientUsage } from "@gram/client/models/components/llmclientusage.js";

let value: LLMClientUsage = {
  activityCount: 386720,
  clientName: "<value>",
};
```

## Fields

| Field           | Type     | Required           | Description                                                      |
| --------------- | -------- | ------------------ | ---------------------------------------------------------------- |
| `activityCount` | _number_ | :heavy_check_mark: | Number of messages (session mode) or tool calls (tool_call mode) |
| `clientName`    | _string_ | :heavy_check_mark: | Client/agent name (e.g., 'cursor', 'claude-code', 'cowork')      |
