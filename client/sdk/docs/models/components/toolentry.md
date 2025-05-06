# ToolEntry

## Example Usage

```typescript
import { ToolEntry } from "@gram/client/models/components";

let value: ToolEntry = {
  createdAt: new Date("2025-09-01T20:12:39.588Z"),
  deploymentId: "<id>",
  id: "<id>",
  name: "<value>",
  openapiv3DocumentId: "<id>",
  summary: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the tool.                                                                |
| `deploymentId`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | The deployment ID                                                                             |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool ID                                                                                   |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool name                                                                                 |
| `openapiv3DocumentId`                                                                         | *string*                                                                                      | :heavy_check_mark:                                                                            | The OpenAPI v3 document ID                                                                    |
| `packageName`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The package name                                                                              |
| `summary`                                                                                     | *string*                                                                                      | :heavy_check_mark:                                                                            | The tool summary                                                                              |