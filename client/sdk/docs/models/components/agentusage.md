# AgentUsage

## Example Usage

```typescript
import { AgentUsage } from "@gram/client/models/components/agentusage.js";

let value: AgentUsage = {
  type: "claude",
};
```

## Fields

| Field    | Type                                                                       | Required           | Description                            |
| -------- | -------------------------------------------------------------------------- | ------------------ | -------------------------------------- |
| `claude` | [components.ClaudeAgentUsage](../../models/components/claudeagentusage.md) | :heavy_minus_sign: | N/A                                    |
| `type`   | [components.Type](../../models/components/type.md)                         | :heavy_check_mark: | The agent usage payload discriminator. |
