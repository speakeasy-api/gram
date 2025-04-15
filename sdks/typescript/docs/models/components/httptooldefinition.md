# HTTPToolDefinition

## Example Usage

```typescript
import { HTTPToolDefinition } from "@gram/sdk/models/components";

let value: HTTPToolDefinition = {
  createdAt: new Date("2025-03-04T09:26:20.697Z"),
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
  tags: [
    "<value>",
  ],
  updatedAt: new Date("2023-01-25T17:59:41.729Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the deployment                                                                      |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | Description of the tool                                                                       |
| `httpMethod`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | HTTP method for the request                                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the HTTP tool                                                                       |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the tool                                                                          |
| `openapiv3DocumentId`                                                                         | *string*                                                                                      | :heavy_minus_sign:                                                                            | The ID of the OpenAPI v3 document                                                             |
| `openapiv3Operation`                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | OpenAPI v3 operation                                                                          |
| `path`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Path for the request                                                                          |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the project                                                                         |
| `schema`                                                                                      | *string*                                                                                      | :heavy_check_mark:                                                                            | JSON schema for the request                                                                   |
| `schemaVersion`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | Version of the schema                                                                         |
| `security`                                                                                    | *string*                                                                                      | :heavy_minus_sign:                                                                            | Security requirements for the underlying HTTP endpoint                                        |
| `summary`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | Summary of the tool                                                                           |
| `tags`                                                                                        | *string*[]                                                                                    | :heavy_check_mark:                                                                            | The tags list for this http tool                                                              |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The last update date of the tool.                                                             |