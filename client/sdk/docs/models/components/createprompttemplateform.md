# CreatePromptTemplateForm

## Example Usage

```typescript
import { CreatePromptTemplateForm } from "@gram/client/models/components";

let value: CreatePromptTemplateForm = {
  engine: "mustache",
  kind: "higher_order_tool",
  name: "<value>",
  prompt: "<value>",
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `arguments`                                                                                            | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The JSON Schema defining the placeholders found in the prompt template                                 |
| `description`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The description of the prompt template                                                                 |
| `engine`                                                                                               | [components.CreatePromptTemplateFormEngine](../../models/components/createprompttemplateformengine.md) | :heavy_check_mark:                                                                                     | The template engine                                                                                    |
| `kind`                                                                                                 | [components.CreatePromptTemplateFormKind](../../models/components/createprompttemplateformkind.md)     | :heavy_check_mark:                                                                                     | The kind of prompt the template is used for                                                            |
| `name`                                                                                                 | *string*                                                                                               | :heavy_check_mark:                                                                                     | A short url-friendly label that uniquely identifies a resource.                                        |
| `prompt`                                                                                               | *string*                                                                                               | :heavy_check_mark:                                                                                     | The template content                                                                                   |
| `toolUrnsHint`                                                                                         | *string*[]                                                                                             | :heavy_minus_sign:                                                                                     | The suggested tool URNS associated with the prompt template                                            |
| `toolsHint`                                                                                            | *string*[]                                                                                             | :heavy_minus_sign:                                                                                     | The suggested tool names associated with the prompt template                                           |