# CreatePromptTemplateResult

## Example Usage

```typescript
import { CreatePromptTemplateResult } from "@gram/client/models/components";

let value: CreatePromptTemplateResult = {
  template: {
    canonicalName: "<value>",
    createdAt: new Date("2026-02-04T03:43:35.518Z"),
    description: "ha swathe dental an evil",
    engine: "mustache",
    historyId: "<id>",
    id: "<id>",
    kind: "prompt",
    name: "<value>",
    projectId: "<id>",
    prompt: "<value>",
    schema: "<value>",
    toolUrn: "<value>",
    toolsHint: [
      "<value 1>",
      "<value 2>",
      "<value 3>",
    ],
    updatedAt: new Date("2026-12-12T05:35:57.442Z"),
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `template`                                                             | [components.PromptTemplate](../../models/components/prompttemplate.md) | :heavy_check_mark:                                                     | A prompt template                                                      |