# HTTPToolDefinition

An HTTP tool

## Example Usage

```typescript
import { HTTPToolDefinition } from "@gram/client/models/components/httptooldefinition.js";

let value: HTTPToolDefinition = {
  assetId: "<id>",
  canonicalName: "<value>",
  createdAt: new Date("2026-03-04T09:26:20.697Z"),
  deploymentId: "<id>",
  description:
    "winding oh burly lest notwithstanding viciously curiously swathe a atop",
  httpMethod: "<value>",
  id: "<id>",
  name: "<value>",
  path: "/opt/share",
  projectId: "<id>",
  schema: "<value>",
  summary: "<value>",
  tags: [],
  toolUrn: "<value>",
  updatedAt: new Date("2026-05-22T21:11:55.475Z"),
};
```

## Fields

| Field                 | Type                                                                                          | Required           | Description                                                                            |
| --------------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------- |
| `annotations`         | [components.ToolAnnotations](../../models/components/toolannotations.md)                      | :heavy_minus_sign: | Tool annotations providing behavioral hints about the tool                             |
| `assetId`             | _string_                                                                                      | :heavy_check_mark: | The ID of the asset                                                                    |
| `canonical`           | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign: | The original details of a tool                                                         |
| `canonicalName`       | _string_                                                                                      | :heavy_check_mark: | The canonical name of the tool. Will be the same as the name if there is no variation. |
| `confirm`             | _string_                                                                                      | :heavy_minus_sign: | Confirmation mode for the tool                                                         |
| `confirmPrompt`       | _string_                                                                                      | :heavy_minus_sign: | Prompt for the confirmation                                                            |
| `createdAt`           | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the tool.                                                         |
| `defaultServerUrl`    | _string_                                                                                      | :heavy_minus_sign: | The default server URL for the tool                                                    |
| `deploymentId`        | _string_                                                                                      | :heavy_check_mark: | The ID of the deployment                                                               |
| `description`         | _string_                                                                                      | :heavy_check_mark: | Description of the tool                                                                |
| `httpMethod`          | _string_                                                                                      | :heavy_check_mark: | HTTP method for the request                                                            |
| `id`                  | _string_                                                                                      | :heavy_check_mark: | The ID of the tool                                                                     |
| `name`                | _string_                                                                                      | :heavy_check_mark: | The name of the tool                                                                   |
| `openapiv3DocumentId` | _string_                                                                                      | :heavy_minus_sign: | The ID of the OpenAPI v3 document                                                      |
| `openapiv3Operation`  | _string_                                                                                      | :heavy_minus_sign: | OpenAPI v3 operation                                                                   |
| `packageName`         | _string_                                                                                      | :heavy_minus_sign: | The name of the source package                                                         |
| `path`                | _string_                                                                                      | :heavy_check_mark: | Path for the request                                                                   |
| `projectId`           | _string_                                                                                      | :heavy_check_mark: | The ID of the project                                                                  |
| `responseFilter`      | [components.ResponseFilter](../../models/components/responsefilter.md)                        | :heavy_minus_sign: | Response filter metadata for the tool                                                  |
| `schema`              | _string_                                                                                      | :heavy_check_mark: | JSON schema for the request                                                            |
| `schemaVersion`       | _string_                                                                                      | :heavy_minus_sign: | Version of the schema                                                                  |
| `security`            | _string_                                                                                      | :heavy_minus_sign: | Security requirements for the underlying HTTP endpoint                                 |
| `summarizer`          | _string_                                                                                      | :heavy_minus_sign: | Summarizer for the tool                                                                |
| `summary`             | _string_                                                                                      | :heavy_check_mark: | Summary of the tool                                                                    |
| `tags`                | _string_[]                                                                                    | :heavy_check_mark: | The tags list for this http tool                                                       |
| `toolUrn`             | _string_                                                                                      | :heavy_check_mark: | The URN of this tool                                                                   |
| `updatedAt`           | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the tool.                                                      |
| `variation`           | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign: | N/A                                                                                    |
