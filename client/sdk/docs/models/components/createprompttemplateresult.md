# CreatePromptTemplateResult

## Example Usage

```typescript
import { CreatePromptTemplateResult } from "@gram/client/models/components";

let value: CreatePromptTemplateResult = {
  template: {
    createdAt: new Date("2025-02-04T03:43:35.518Z"),
    engine: "mustache",
    historyId: "<id>",
    id: "<id>",
    kind: "prompt",
    name: "<value>",
    prompt: "<value>",
    toolsHint: [
      "<value>",
    ],
    updatedAt: new Date("2024-10-03T12:01:38.067Z"),
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `template`                                                             | [components.PromptTemplate](../../models/components/prompttemplate.md) | :heavy_check_mark:                                                     | N/A                                                                    |