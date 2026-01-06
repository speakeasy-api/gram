# PromptTemplate

A prompt template

## Example Usage

```typescript
import { PromptTemplate } from "@gram/client/models/components";

let value: PromptTemplate = {
  canonicalName: "<value>",
  createdAt: new Date("2025-03-05T06:53:41.866Z"),
  description: "as impartial into even lavish",
  engine: "mustache",
  historyId: "<id>",
  id: "<id>",
  kind: "prompt",
  name: "<value>",
  projectId: "<id>",
  prompt: "<value>",
  schema: "<value>",
  toolUrn: "<value>",
  toolsHint: [],
  updatedAt: new Date("2026-12-07T09:02:53.100Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `canonical`                                                                                   | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign:                                                                            | The original details of a tool                                                                |
| `canonicalName`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | The canonical name of the tool. Will be the same as the name if there is no variation.        |
| `confirm`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | Confirmation mode for the tool                                                                |
| `confirmPrompt`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Prompt for the confirmation                                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `engine`                                                                                      | [components.Engine](../../models/components/engine.md)                                        | :heavy_check_mark:                                                                            | The template engine                                                                           |
| `historyId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The revision tree ID for the prompt template                                                  |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the tool                                                                            |
| `kind`                                                                                        | [components.PromptTemplateKind](../../models/components/prompttemplatekind.md)                | :heavy_check_mark:                                                                            | The kind of prompt the template is used for                                                   |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `predecessorId`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The previous version of the prompt template to use as predecessor                             |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `prompt`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | The template content                                                                          |
| `schema`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | JSON schema for the request                                                                   |
| `schemaVersion`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Version of the schema                                                                         |
| `summarizer`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Summarizer for the tool                                                                       |
| `toolUrn`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The URN of this tool                                                                          |
| `toolUrnsHint`                                                                                | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The suggested tool URNS associated with the prompt template                                   |
| `toolsHint`                                                                                   | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The suggested tool names associated with the prompt template                                  |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |
| `variation`                                                                                   | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign:                                                                            | N/A                                                                                           |