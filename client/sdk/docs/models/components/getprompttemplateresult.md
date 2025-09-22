# GetPromptTemplateResult

## Example Usage

```typescript
import { GetPromptTemplateResult } from "@gram/client/models/components";

let value: GetPromptTemplateResult = {
  template: {
    createdAt: new Date("2025-02-04T03:43:35.518Z"),
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
    updatedAt: new Date("2025-01-13T13:49:49.606Z"),
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `template`                                                             | [components.PromptTemplate](../../models/components/prompttemplate.md) | :heavy_check_mark:                                                     | N/A                                                                    |