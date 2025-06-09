# GetPromptTemplateResult

## Example Usage

```typescript
import { GetPromptTemplateResult } from "@gram/client/models/components";

let value: GetPromptTemplateResult = {
  template: {
    createdAt: new Date("2025-03-25T11:16:14.375Z"),
    engine: "mustache",
    historyId: "<id>",
    id: "<id>",
    kind: "prompt",
    name: "<value>",
    prompt: "<value>",
    toolsHint: [
      "<value>",
    ],
    updatedAt: new Date("2025-04-20T22:03:57.587Z"),
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `template`                                                             | [components.PromptTemplate](../../models/components/prompttemplate.md) | :heavy_check_mark:                                                     | N/A                                                                    |