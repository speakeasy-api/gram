# ListToolsResult

## Example Usage

```typescript
import { ListToolsResult } from "@gram/client/models/components";

let value: ListToolsResult = {
  httpTools: [
    {
      canonicalName: "<value>",
      confirm: "<value>",
      createdAt: new Date("2024-04-12T15:22:32.769Z"),
      deploymentId: "<id>",
      description: "near impassioned including abaft painfully",
      httpMethod: "<value>",
      id: "<id>",
      name: "<value>",
      path: "/home/user/dir",
      projectId: "<id>",
      schema: "<value>",
      summary: "<value>",
      tags: [
        "<value 1>",
      ],
      toolType: "http",
      toolUrn: "<value>",
      updatedAt: new Date("2023-12-14T09:29:13.684Z"),
    },
  ],
  promptTemplates: [
    {
      createdAt: new Date("2023-08-31T15:47:56.029Z"),
      engine: "mustache",
      historyId: "<id>",
      id: "<id>",
      kind: "prompt",
      name: "<value>",
      prompt: "<value>",
      toolUrn: "<value>",
      toolsHint: [
        "<value 1>",
        "<value 2>",
      ],
      updatedAt: new Date("2025-06-28T14:34:38.671Z"),
    },
  ],
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `httpTools`                                                                      | [components.HTTPToolDefinition](../../models/components/httptooldefinition.md)[] | :heavy_check_mark:                                                               | The list of HTTP tools                                                           |
| `nextCursor`                                                                     | *string*                                                                         | :heavy_minus_sign:                                                               | The cursor to fetch results from                                                 |
| `promptTemplates`                                                                | [components.PromptTemplate](../../models/components/prompttemplate.md)[]         | :heavy_check_mark:                                                               | The list of prompt templates                                                     |