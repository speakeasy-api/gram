# UpdatePromptTemplateResult

## Example Usage

```typescript
import { UpdatePromptTemplateResult } from "@gram/client/models/components";

let value: UpdatePromptTemplateResult = {
  template: {
    createdAt: new Date("2023-07-07T11:52:39.775Z"),
    engine: "mustache",
    historyId: "<id>",
    id: "<id>",
    kind: "higher_order_tool",
    name: "<value>",
    prompt: "<value>",
    toolsHint: [
      "<value>",
    ],
    updatedAt: new Date("2025-04-25T21:00:08.895Z"),
  },
};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `template`                                                             | [components.PromptTemplate](../../models/components/prompttemplate.md) | :heavy_check_mark:                                                     | N/A                                                                    |