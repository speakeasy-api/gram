# ListPromptTemplatesResult

## Example Usage

```typescript
import { ListPromptTemplatesResult } from "@gram/client/models/components";

let value: ListPromptTemplatesResult = {
  templates: [
    {
      createdAt: new Date("2023-12-12T07:43:42.447Z"),
      engine: "mustache",
      historyId: "<id>",
      id: "<id>",
      kind: "prompt",
      name: "<value>",
      prompt: "<value>",
      toolsHint: [
        "<value>",
      ],
      updatedAt: new Date("2025-03-07T04:16:04.500Z"),
    },
  ],
};
```

## Fields

| Field                                                                    | Type                                                                     | Required                                                                 | Description                                                              |
| ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ | ------------------------------------------------------------------------ |
| `templates`                                                              | [components.PromptTemplate](../../models/components/prompttemplate.md)[] | :heavy_check_mark:                                                       | The created prompt template                                              |