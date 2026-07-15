# PlatformToolDefinition

A platform-owned tool served directly by the platform

## Example Usage

```typescript
import { PlatformToolDefinition } from "@gram/client/models/components/platformtooldefinition.js";

let value: PlatformToolDefinition = {
  canonicalName: "<value>",
  createdAt: new Date("2025-01-11T00:41:23.999Z"),
  description: "rule given indeed dress",
  id: "<id>",
  name: "<value>",
  projectId: "<id>",
  schema: "<value>",
  sourceSlug: "<value>",
  toolUrn: "<value>",
  updatedAt: new Date("2024-05-28T21:54:02.545Z"),
};
```

## Fields

| Field           | Type                                                                                          | Required           | Description                                                                            |
| --------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------- |
| `annotations`   | [components.ToolAnnotations](../../models/components/toolannotations.md)                      | :heavy_minus_sign: | Tool annotations providing behavioral hints about the tool                             |
| `canonical`     | [components.CanonicalToolAttributes](../../models/components/canonicaltoolattributes.md)      | :heavy_minus_sign: | The original details of a tool                                                         |
| `canonicalName` | _string_                                                                                      | :heavy_check_mark: | The canonical name of the tool. Will be the same as the name if there is no variation. |
| `confirm`       | _string_                                                                                      | :heavy_minus_sign: | Confirmation mode for the tool                                                         |
| `confirmPrompt` | _string_                                                                                      | :heavy_minus_sign: | Prompt for the confirmation                                                            |
| `createdAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the tool.                                                         |
| `description`   | _string_                                                                                      | :heavy_check_mark: | Description of the tool                                                                |
| `id`            | _string_                                                                                      | :heavy_check_mark: | The ID of the tool                                                                     |
| `name`          | _string_                                                                                      | :heavy_check_mark: | The name of the tool                                                                   |
| `ownerId`       | _string_                                                                                      | :heavy_minus_sign: | Optional owning entity ID                                                              |
| `ownerKind`     | _string_                                                                                      | :heavy_minus_sign: | The entity kind that owns this tool's lifecycle                                        |
| `projectId`     | _string_                                                                                      | :heavy_check_mark: | The ID of the project                                                                  |
| `schema`        | _string_                                                                                      | :heavy_check_mark: | JSON schema for the request                                                            |
| `schemaVersion` | _string_                                                                                      | :heavy_minus_sign: | Version of the schema                                                                  |
| `sourceSlug`    | _string_                                                                                      | :heavy_check_mark: | The backing platform tool source (for example: logs)                                   |
| `summarizer`    | _string_                                                                                      | :heavy_minus_sign: | Summarizer for the tool                                                                |
| `toolUrn`       | _string_                                                                                      | :heavy_check_mark: | The URN of this tool                                                                   |
| `updatedAt`     | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the tool.                                                      |
| `variation`     | [components.ToolVariation](../../models/components/toolvariation.md)                          | :heavy_minus_sign: | N/A                                                                                    |
