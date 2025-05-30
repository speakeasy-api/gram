# ToolEntry

## Example Usage

```typescript
import { ToolEntry } from "@gram/client/models/components";

let value: ToolEntry = {
  confirm: "<value>",
  createdAt: new Date("2025-09-01T20:12:39.588Z"),
  deploymentId: "<id>",
  description:
    "meh viciously designation clinking unconscious decode segregate pfft",
  httpMethod: "<value>",
  id: "<id>",
  name: "<value>",
  path: "/var/tmp",
  projectId: "<id>",
  schema: "<value>",
  summary: "<value>",
  tags: [
    "<value>",
  ],
  updatedAt: new Date("2023-05-10T18:26:05.224Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `canonical`                                                                                   | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign:                                                                            | The original details of a tool                                                                |
| `confirm`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Confirmation mode for the tool                                                                |
| `confirmPrompt`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Prompt for the confirmation                                                                   |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment                                                                      |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `httpMethod`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP method for the request                                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the HTTP tool                                                                       |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `openapiv3DocumentId`                                                                         | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the OpenAPI v3 document                                                             |
| `openapiv3Operation`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | OpenAPI v3 operation                                                                          |
| `packageName`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The package name                                                                              |
| `path`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Path for the request                                                                          |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `schema`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | JSON schema for the request                                                                   |
| `schemaVersion`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Version of the schema                                                                         |
| `security`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | Security requirements for the underlying HTTP endpoint                                        |
| `summarizer`                                                                                  | *string*                                                                                      | :heavy_minus_sign:                                                                            | Summarizer for the tool                                                                       |
| `summary`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Summary of the tool                                                                           |
| `tags`                                                                                        | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The tags list for this http tool                                                              |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |
| `variation`                                                                                   | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign:                                                                            | N/A                                                                                           |