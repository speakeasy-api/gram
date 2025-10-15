# UpdatePromptTemplateForm

## Example Usage

```typescript
import { UpdatePromptTemplateForm } from "@gram/client/models/components";

let value: UpdatePromptTemplateForm = {
  id: "<id>",
};
```

## Fields

| Field                                                                                                  | Type                                                                                                   | Required                                                                                               | Description                                                                                            |
| ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------ |
| `arguments`                                                                                            | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The JSON Schema defining the placeholders found in the prompt template                                 |
| `description`                                                                                          | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The description of the prompt template                                                                 |
| `engine`                                                                                               | [components.UpdatePromptTemplateFormEngine](../../models/components/updateprompttemplateformengine.md) | :heavy_minus_sign:                                                                                     | The template engine                                                                                    |
| `id`                                                                                                   | *string*                                                                                               | :heavy_check_mark:                                                                                     | The ID of the prompt template to update                                                                |
| `kind`                                                                                                 | [components.UpdatePromptTemplateFormKind](../../models/components/updateprompttemplateformkind.md)     | :heavy_minus_sign:                                                                                     | The kind of prompt the template is used for                                                            |
| `name`                                                                                                 | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The name of the prompt template. Will be updated via variation                                         |
| `prompt`                                                                                               | *string*                                                                                               | :heavy_minus_sign:                                                                                     | The template content                                                                                   |
| `toolsHint`                                                                                            | *string*[]                                                                                             | :heavy_minus_sign:                                                                                     | The suggested tool names associated with the prompt template                                           |