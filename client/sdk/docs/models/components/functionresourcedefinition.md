# FunctionResourceDefinition

A function resource

## Example Usage

```typescript
import { FunctionResourceDefinition } from "@gram/client/models/components/functionresourcedefinition.js";

let value: FunctionResourceDefinition = {
  createdAt: new Date("2025-05-21T09:10:14.826Z"),
  deploymentId: "<id>",
  description: "backbone wherever emphasize or gee rejigger amid valiantly",
  functionId: "<id>",
  id: "<id>",
  name: "<value>",
  projectId: "<id>",
  resourceUrn: "<value>",
  runtime: "<value>",
  updatedAt: new Date("2025-10-02T17:48:33.775Z"),
  uri: "https://sleepy-slipper.net",
};
```

## Fields

| Field          | Type                                                                                          | Required           | Description                                        |
| -------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------- |
| `createdAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The creation date of the resource.                 |
| `deploymentId` | _string_                                                                                      | :heavy_check_mark: | The ID of the deployment                           |
| `description`  | _string_                                                                                      | :heavy_check_mark: | Description of the resource                        |
| `functionId`   | _string_                                                                                      | :heavy_check_mark: | The ID of the function                             |
| `id`           | _string_                                                                                      | :heavy_check_mark: | The ID of the resource                             |
| `meta`         | Record<string, _any_>                                                                         | :heavy_minus_sign: | Meta tags for the tool                             |
| `mimeType`     | _string_                                                                                      | :heavy_minus_sign: | Optional MIME type of the resource                 |
| `name`         | _string_                                                                                      | :heavy_check_mark: | The name of the resource                           |
| `projectId`    | _string_                                                                                      | :heavy_check_mark: | The ID of the project                              |
| `resourceUrn`  | _string_                                                                                      | :heavy_check_mark: | The URN of this resource                           |
| `runtime`      | _string_                                                                                      | :heavy_check_mark: | Runtime environment (e.g., nodejs:24, python:3.12) |
| `title`        | _string_                                                                                      | :heavy_minus_sign: | Optional title for the resource                    |
| `updatedAt`    | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | The last update date of the resource.              |
| `uri`          | _string_                                                                                      | :heavy_check_mark: | The URI of the resource                            |
| `variables`    | _any_                                                                                         | :heavy_minus_sign: | Variables configuration for the resource           |
