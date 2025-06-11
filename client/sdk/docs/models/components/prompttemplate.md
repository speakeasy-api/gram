# PromptTemplate

## Example Usage

```typescript
import { PromptTemplate } from "@gram/client/models/components";

let value: PromptTemplate = {
  createdAt: new Date("2024-03-05T06:53:41.866Z"),
  engine: "mustache",
  historyId: "<id>",
  id: "<id>",
  kind: "prompt",
  name: "<value>",
  prompt: "<value>",
  toolsHint: [
    "<value>",
  ],
  updatedAt: new Date("2024-11-07T17:04:28.929Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `arguments`                                                                                   | *string*                                                                                      | :heavy_minus_sign:                                                                            | The JSON Schema defining the placeholders found in the prompt template                        |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the prompt template.                                                     |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The description of the prompt template                                                        |
| `engine`                                                                                      | [components.Engine](../../models/components/engine.md)                                        | :heavy_check_mark:                                                                            | The template engine                                                                           |
| `historyId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The revision tree ID for the prompt template                                                  |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the prompt template                                                                 |
| `kind`                                                                                        | [components.PromptTemplateKind](../../models/components/prompttemplatekind.md)                | :heavy_check_mark:                                                                            | The kind of prompt the template is used for                                                   |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `predecessorId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The previous version of the prompt template to use as predecessor                             |
| `prompt`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The template content                                                                          |
| `toolsHint`                                                                                   | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The suggested tool names associated with the prompt template                                  |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the prompt template.                                                  |