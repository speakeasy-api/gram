# HTTPToolDefinition

An HTTP tool

## Example Usage

```typescript
import { HTTPToolDefinition } from "@gram/client/models/components";

let value: HTTPToolDefinition = {
  canonicalName: "<value>",
  confirm: "<value>",
  createdAt: new Date("2024-07-12T21:04:02.837Z"),
  deploymentId: "<id>",
  description: "since character yogurt freely yet substitution essential",
  httpMethod: "<value>",
  id: "<id>",
  name: "<value>",
  path: "/boot",
  projectId: "<id>",
  schema: "<value>",
  summary: "<value>",
  tags: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
  toolUrn: "<value>",
  type: "http",
  updatedAt: new Date("2025-06-27T15:50:36.598Z"),
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
| `defaultServerUrl`                                                                            | *string*                                                                                      | :heavy_minus_sign:                                                                            | The default server URL for the tool                                                           |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment                                                                      |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `httpMethod`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP method for the request                                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the tool                                                                            |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `openapiv3DocumentId`                                                                         | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the OpenAPI v3 document                                                             |
| `openapiv3Operation`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | OpenAPI v3 operation                                                                          |
| `packageName`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The name of the source package                                                                |
| `path`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Path for the request                                                                          |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `responseFilter`                                                                              | [components.ResponseFilter](../../models/components/responsefilter.md)                        | :heavy_minus_sign:                                                                            | Response filter metadata for the tool                                                         |
| `schema`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | JSON schema for the request                                                                   |
| `schemaVersion`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Version of the schema                                                                         |
| `security`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | Security requirements for the underlying HTTP endpoint                                        |
| `summarizer`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Summarizer for the tool                                                                       |
| `summary`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Summary of the tool                                                                           |
| `tags`                                                                                        | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The tags list for this http tool                                                              |
| `toolUrn`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The URN of this tool                                                                          |
| `type`                                                                                        | [components.HTTPToolDefinitionType](../../models/components/httptooldefinitiontype.md)        | :heavy_check_mark:                                                                            | The type of the tool - discriminator value                                                    |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |
| `variation`                                                                                   | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign:                                                                            | N/A                                                                                           |