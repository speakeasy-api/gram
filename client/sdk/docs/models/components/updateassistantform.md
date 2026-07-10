# UpdateAssistantForm

## Example Usage

```typescript
import { UpdateAssistantForm } from "@gram/client/models/components/updateassistantform.js";

let value: UpdateAssistantForm = {
  id: "2bcdb23f-bd33-4d94-a5de-1293842b90d5",
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `id`                                                                                         | *string*                                                                                     | :heavy_check_mark:                                                                           | The assistant ID.                                                                            |
| `instructions`                                                                               | *string*                                                                                     | :heavy_minus_sign:                                                                           | The system instructions for the assistant.                                                   |
| `maxConcurrency`                                                                             | *number*                                                                                     | :heavy_minus_sign:                                                                           | Maximum active warm runtimes.                                                                |
| `mcpServers`                                                                                 | [components.AssistantMCPServerRef](../../models/components/assistantmcpserverref.md)[]       | :heavy_minus_sign:                                                                           | MCP servers attached directly to the assistant (remote- or tunnelled-backed).                |
| `model`                                                                                      | *string*                                                                                     | :heavy_minus_sign:                                                                           | The model identifier used by the assistant.                                                  |
| `name`                                                                                       | *string*                                                                                     | :heavy_minus_sign:                                                                           | The assistant name.                                                                          |
| `status`                                                                                     | [components.UpdateAssistantFormStatus](../../models/components/updateassistantformstatus.md) | :heavy_minus_sign:                                                                           | The assistant status.                                                                        |
| `toolsets`                                                                                   | [components.AssistantToolsetRef](../../models/components/assistanttoolsetref.md)[]           | :heavy_minus_sign:                                                                           | Toolsets available to the assistant.                                                         |
| `warmTtlSeconds`                                                                             | *number*                                                                                     | :heavy_minus_sign:                                                                           | Warm runtime TTL in seconds.                                                                 |