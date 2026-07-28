# FunctionToolDefinition

A function tool

## Example Usage

```typescript
import { FunctionToolDefinition } from "@gram/client/models/components/functiontooldefinition.js";

let value: FunctionToolDefinition = {
  assetId: "<id>",
  canonicalName: "<value>",
  createdAt: new Date("2024-08-27T10:42:53.622Z"),
  deploymentId: "<id>",
  description: "scruple whether yahoo",
  functionId: "<id>",
  id: "<id>",
  name: "<value>",
  projectId: "<id>",
  runtime: "<value>",
  schema: "<value>",
  tags: ["<value 1>"],
  toolUrn: "<value>",
  updatedAt: new Date("2024-07-25T21:58:59.496Z"),
};
```

## Fields

| Field           | Type                                                                                          | Required           | Description                                                                            |
| --------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------- |
| `annotations`   | [components.ToolAnnotations](../../models/components/toolannotations.md)                      | :heavy_minus_sign: | Tool annotations providing behavioral hints about the tool                             |
| `assetId`       | _string_                                                                                      | :heavy_check_mark: | The ID of the asset                                                                    |
| `canonical`     | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign: | The original details of a tool                                                         |
| `canonicalName` | _string_                                                                                      | :heavy_check_mark: | The canonical name of the tool. Will be the same as the name if there is no variation. |
| `confirm`       | _string_                                                                                      | :heavy_minus_sign: | Confirmation mode for the tool                                                         |
| `confirmPrompt` | _string_                                                                                      | :heavy_minus_sign: | Prompt for the confirmation                                                            |
| `createdAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the tool.                                                         |
| `deploymentId`  | _string_                                                                                      | :heavy_check_mark: | The ID of the deployment                                                               |
| `description`   | _string_                                                                                      | :heavy_check_mark: | Description of the tool                                                                |
| `functionId`    | _string_                                                                                      | :heavy_check_mark: | The ID of the function                                                                 |
| `id`            | _string_                                                                                      | :heavy_check_mark: | The ID of the tool                                                                     |
| `meta`          | Record<string, _any_>                                                                         | :heavy_minus_sign: | Meta tags for the tool                                                                 |
| `name`          | _string_                                                                                      | :heavy_check_mark: | The name of the tool                                                                   |
| `projectId`     | _string_                                                                                      | :heavy_check_mark: | The ID of the project                                                                  |
| `runtime`       | _string_                                                                                      | :heavy_check_mark: | Runtime environment (e.g., nodejs:24, python:3.12)                                     |
| `schema`        | _string_                                                                                      | :heavy_check_mark: | JSON schema for the request                                                            |
| `schemaVersion` | _string_                                                                                      | :heavy_minus_sign: | Version of the schema                                                                  |
| `summarizer`    | _string_                                                                                      | :heavy_minus_sign: | Summarizer for the tool                                                                |
| `tags`          | _string_[]                                                                                    | :heavy_check_mark: | The tags list for this function tool                                                   |
| `toolUrn`       | _string_                                                                                      | :heavy_check_mark: | The URN of this tool                                                                   |
| `updatedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the tool.                                                      |
| `variables`     | _any_                                                                                         | :heavy_minus_sign: | Variables configuration for the function                                               |
| `variation`     | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign: | N/A                                                                                    |
