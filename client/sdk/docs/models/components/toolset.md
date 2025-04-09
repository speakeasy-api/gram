# Toolset

## Example Usage

```typescript
import { Toolset } from "@gram/sdk/models/components";

let value: Toolset = {
  createdAt: new Date("2025-09-03T11:41:50.334Z"),
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2024-05-12T19:09:07.564Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was created.                                                                 |
| `defaultEnvironmentSlug`                                                                      | *string*                                                                                      | :heavy_minus_sign:                                                                            | The slug of the environment to use as the default for the toolset                             |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Description of the toolset                                                                    |
| `httpToolNames`                                                                               | *string*[]                                                                                    | :heavy_minus_sign:                                                                            | List of HTTP tool names included in this toolset                                              |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the toolset                                                                         |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the toolset                                                                       |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this toolset belongs to                                                   |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this toolset belongs to                                                        |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The slug of the toolset                                                                       |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the toolset was last updated.                                                            |