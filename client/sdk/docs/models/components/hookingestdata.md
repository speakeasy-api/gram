# HookIngestData

Feature-specific payloads. Hooks populate only the blocks needed for the event.

## Example Usage

```typescript
import { HookIngestData } from "@gram/client/models/components/hookingestdata.js";

let value: HookIngestData = {};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `mcp`                                                                              | [components.HookMCPData](../../models/components/hookmcpdata.md)                   | :heavy_minus_sign:                                                                 | MCP feature payload.                                                               |
| `message`                                                                          | [components.HookMessageData](../../models/components/hookmessagedata.md)           | :heavy_minus_sign:                                                                 | Assistant/user message payload.                                                    |
| `notification`                                                                     | [components.HookNotificationData](../../models/components/hooknotificationdata.md) | :heavy_minus_sign:                                                                 | Local agent notification payload.                                                  |
| `prompt`                                                                           | [components.HookPromptData](../../models/components/hookpromptdata.md)             | :heavy_minus_sign:                                                                 | Prompt feature payload.                                                            |
| `skill`                                                                            | [components.HookSkillData](../../models/components/hookskilldata.md)               | :heavy_minus_sign:                                                                 | Skill activation payload.                                                          |
| `toolCall`                                                                         | [components.HookToolCallData](../../models/components/hooktoolcalldata.md)         | :heavy_minus_sign:                                                                 | Tool call feature payload.                                                         |
| `usage`                                                                            | [components.HookUsageData](../../models/components/hookusagedata.md)               | :heavy_minus_sign:                                                                 | Token and cost usage payload.                                                      |