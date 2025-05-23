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
  openapiv3DocumentId: "<id>",
  path: "/var/tmp",
  summary: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `canonical`                                                                                   | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign:                                                                            | The original details of a tool                                                                |
| `confirm`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The confirmation mode for the tool                                                            |
| `confirmPrompt`                                                                               | *string*                                                                                      | :heavy_minus_sign:                                                                            | The confirmation prompt for the tool                                                          |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The deployment ID                                                                             |
| `description`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool description                                                                          |
| `httpMethod`                                                                                  | *string*                                                                                      | :heavy_check_mark:                                                                            | The HTTP method                                                                               |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool ID                                                                                   |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool name                                                                                 |
| `openapiv3DocumentId`                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | The OpenAPI v3 document ID                                                                    |
| `packageName`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The package name                                                                              |
| `path`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The path                                                                                      |
| `summary`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool summary                                                                              |
| `tags`                                                                                        | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | The tags for the tool                                                                         |