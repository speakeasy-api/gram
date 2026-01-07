# FunctionToolDefinition

A function tool

## Example Usage

```typescript
import { FunctionToolDefinition } from "@gram/client/models/components";

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
  toolUrn: "<value>",
  updatedAt: new Date("2025-04-07T23:29:23.396Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `assetId`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the asset                                                                           |
| `canonical`                                                                                   | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign:                                                                            | The original details of a tool                                                                |
| `canonicalName`                                                                               | *string*                                                                                      | :heavy_check_mark:                                                                            | The canonical name of the tool. Will be the same as the name if there is no variation.        |
| `confirm`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | Confirmation mode for the tool                                                                |
| `confirmPrompt`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Prompt for the confirmation                                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment                                                                      |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `functionId`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the function                                                                        |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the tool                                                                            |
| `meta`                                                                                        | Record<string, *any*>                                                                         | :heavy_minus_sign:                                                                            | Meta tags for the tool                                                                        |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `runtime`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Runtime environment (e.g., nodejs:22, python:3.12)                                            |
| `schema`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | JSON schema for the request                                                                   |
| `schemaVersion`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Version of the schema                                                                         |
| `summarizer`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Summarizer for the tool                                                                       |
| `toolUrn`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The URN of this tool                                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |
| `variables`                                                                                   | *any*                                                                                         | :heavy_minus_sign:                                                                            | Variables configuration for the function                                                      |
| `variation`                                                                                   | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign:                                                                            | N/A                                                                                           |