# HookIngestData

Feature-specific payloads. Hooks populate only the blocks needed for the event.

## Fields

| Field          | Type                                                                                 | Required           | Description                       |
| -------------- | ------------------------------------------------------------------------------------ | ------------------ | --------------------------------- |
| `Mcp`          | [\*components.HookMCPData](../../models/components/hookmcpdata.md)                   | :heavy_minus_sign: | MCP feature payload.              |
| `Message`      | [\*components.HookMessageData](../../models/components/hookmessagedata.md)           | :heavy_minus_sign: | Assistant/user message payload.   |
| `Notification` | [\*components.HookNotificationData](../../models/components/hooknotificationdata.md) | :heavy_minus_sign: | Local agent notification payload. |
| `Prompt`       | [\*components.HookPromptData](../../models/components/hookpromptdata.md)             | :heavy_minus_sign: | Prompt feature payload.           |
| `Skill`        | [\*components.HookSkillData](../../models/components/hookskilldata.md)               | :heavy_minus_sign: | Skill activation payload.         |
| `ToolCall`     | [\*components.HookToolCallData](../../models/components/hooktoolcalldata.md)         | :heavy_minus_sign: | Tool call feature payload.        |
| `Usage`        | [\*components.HookUsageData](../../models/components/hookusagedata.md)               | :heavy_minus_sign: | Token and cost usage payload.     |
