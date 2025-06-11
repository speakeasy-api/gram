# CreateTemplateRequest

## Example Usage

```typescript
import { CreateTemplateRequest } from "@gram/client/models/operations";

let value: CreateTemplateRequest = {
  createPromptTemplateForm: {
    engine: "mustache",
    kind: "higher_order_tool",
    name: "<value>",
    prompt: "<value>",
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramKey`                                                                                  | *string*                                                                                   | :heavy_minus_sign:                                                                         | API Key header                                                                             |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `createPromptTemplateForm`                                                                 | [components.CreatePromptTemplateForm](../../models/components/createprompttemplateform.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |