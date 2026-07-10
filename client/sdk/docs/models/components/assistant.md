# Assistant

## Example Usage

```typescript
import { Assistant } from "@gram/client/models/components/assistant.js";

let value: Assistant = {
  createdAt: new Date("2024-02-11T19:46:18.978Z"),
  id: "66f6a572-cf46-4439-9d6c-2c3810baf309",
  instructions: "<value>",
  maxConcurrency: 391058,
  mcpServers: [],
  model: "Fiesta",
  name: "<value>",
  projectId: "a7667c6d-7c81-42b6-9589-260f085cc812",
  status: "active",
  toolsets: [],
  updatedAt: new Date("2025-06-23T14:16:06.443Z"),
  warmTtlSeconds: 839457,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Creation timestamp.                                                                           |
| `createdByUserId`                                                                             | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the user who created the assistant, if known.                                       |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The assistant ID.                                                                             |
| `instructions`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The system instructions for the assistant.                                                    |
| `maxConcurrency`                                                                              | *number*                                                                                      | :heavy_check_mark:                                                                            | Maximum active warm runtimes for the assistant.                                               |
| `mcpServers`                                                                                  | [components.AssistantMCPServerRef](../../models/components/assistantmcpserverref.md)[]        | :heavy_check_mark:                                                                            | MCP servers attached directly to the assistant (remote- or tunnelled-backed).                 |
| `model`                                                                                       | *string*                                                                                      | :heavy_check_mark:                                                                            | The model identifier used by the assistant.                                                   |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The assistant name.                                                                           |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID owning the assistant.                                                          |
| `status`                                                                                      | [components.AssistantStatus](../../models/components/assistantstatus.md)                      | :heavy_check_mark:                                                                            | The assistant status.                                                                         |
| `toolsets`                                                                                    | [components.AssistantToolsetRef](../../models/components/assistanttoolsetref.md)[]            | :heavy_check_mark:                                                                            | Toolsets available to the assistant.                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Last update timestamp.                                                                        |
| `warmTtlSeconds`                                                                              | *number*                                                                                      | :heavy_check_mark:                                                                            | Warm runtime TTL in seconds.                                                                  |