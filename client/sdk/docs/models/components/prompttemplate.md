# PromptTemplate

A prompt template

## Example Usage

```typescript
import { PromptTemplate } from "@gram/client/models/components";

let value: PromptTemplate = {
  canonicalName: "<value>",
  confirm: "<value>",
  createdAt: new Date("2025-09-26T02:41:42.436Z"),
  deploymentId: "<id>",
  description: "scratch certainly while ajar",
  engine: "mustache",
  historyId: "<id>",
  id: "<id>",
  kind: "higher_order_tool",
  name: "<value>",
  projectId: "<id>",
  prompt: "<value>",
  toolUrn: "<value>",
  toolsHint: [
    "<value 1>",
    "<value 2>",
  ],
  type: "prompt",
  updatedAt: new Date("2024-04-02T03:48:17.332Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `canonical`                                                                                   | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign:                                                                            | The original details of a tool                                                                |
| `canonicalName`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | The canonical name of the tool. Will be the same as the name if there is no variation.        |
| `confirm`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Confirmation mode for the tool                                                                |
| `confirmPrompt`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Prompt for the confirmation                                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment                                                                      |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `engine`                                                                                      | [components.Engine](../../models/components/engine.md)                                        | :heavy_check_mark:                                                                            | The template engine                                                                           |
| `historyId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The revision tree ID for the prompt template                                                  |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the tool                                                                            |
| `kind`                                                                                        | [components.PromptTemplateKind](../../models/components/prompttemplatekind.md)                | :heavy_check_mark:                                                                            | The kind of prompt the template is used for                                                   |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `predecessorId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The previous version of the prompt template to use as predecessor                             |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `prompt`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The template content                                                                          |
| `schema`                                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | JSON schema for the request                                                                   |
| `schemaVersion`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Version of the schema                                                                         |
| `summarizer`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Summarizer for the tool                                                                       |
| `toolUrn`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The URN of this tool                                                                          |
| `toolsHint`                                                                                   | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The suggested tool names associated with the prompt template                                  |
| `type`                                                                                        | [components.Type](../../models/components/type.md)                                            | :heavy_check_mark:                                                                            | The type of the tool - discriminator value                                                    |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |
| `variation`                                                                                   | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign:                                                                            | N/A                                                                                           |