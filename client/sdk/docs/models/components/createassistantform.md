# CreateAssistantForm

## Example Usage

```typescript
import { CreateAssistantForm } from "@gram/client/models/components/createassistantform.js";

let value: CreateAssistantForm = {
  instructions: "<value>",
  model: "Grand Cherokee",
  name: "<value>",
  toolsets: [
    {
      toolsetSlug: "<value>",
    },
  ],
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `instructions`                                                                               | *string*                                                                                     | :heavy_check_mark:                                                                           | The system instructions for the assistant.                                                   |
| `maxConcurrency`                                                                             | *number*                                                                                     | :heavy_minus_sign:                                                                           | Optional maximum active warm runtimes.                                                       |
| `mcpServers`                                                                                 | [components.AssistantMCPServerRef](../../models/components/assistantmcpserverref.md)[]       | :heavy_minus_sign:                                                                           | MCP servers attached directly to the assistant (remote- or tunnelled-backed).                |
| `model`                                                                                      | *string*                                                                                     | :heavy_check_mark:                                                                           | The model identifier used by the assistant.                                                  |
| `name`                                                                                       | *string*                                                                                     | :heavy_check_mark:                                                                           | The assistant name.                                                                          |
| `status`                                                                                     | [components.CreateAssistantFormStatus](../../models/components/createassistantformstatus.md) | :heavy_minus_sign:                                                                           | Optional initial status.                                                                     |
| `toolsets`                                                                                   | [components.AssistantToolsetRef](../../models/components/assistanttoolsetref.md)[]           | :heavy_check_mark:                                                                           | Toolsets available to the assistant.                                                         |
| `warmTtlSeconds`                                                                             | *number*                                                                                     | :heavy_minus_sign:                                                                           | Optional warm runtime TTL in seconds.                                                        |