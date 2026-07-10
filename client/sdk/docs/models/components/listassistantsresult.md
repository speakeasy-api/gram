# ListAssistantsResult

## Example Usage

```typescript
import { ListAssistantsResult } from "@gram/client/models/components/listassistantsresult.js";

let value: ListAssistantsResult = {
  assistants: [
    {
      createdAt: new Date("2024-05-22T09:23:07.959Z"),
      id: "1fdf2a65-1a33-478c-87da-18f555520eb4",
      instructions: "<value>",
      maxConcurrency: 330326,
      mcpServers: [],
      model: "2",
      name: "<value>",
      projectId: "800da2ae-f3fe-4e60-9ff4-afbac01a1f4c",
      status: "active",
      toolsets: [
        {
          toolsetSlug: "<value>",
        },
      ],
      updatedAt: new Date("2026-11-17T14:35:56.711Z"),
      warmTtlSeconds: 644826,
    },
  ],
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `assistants`                                                   | [components.Assistant](../../models/components/assistant.md)[] | :heavy_check_mark:                                             | Assistants for the current project.                            |