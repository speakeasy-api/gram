# Environment

Model representing an environment

## Example Usage

```typescript
import { Environment } from "@gram/client/models/components";

let value: Environment = {
  createdAt: new Date("2025-12-03T14:12:16.809Z"),
  entries: [],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2024-09-15T02:48:31.195Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | The creation date of the environment                                                          |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | The description of the environment                                                            |
| `entries`                                                                                     | [components.EnvironmentEntry](../../models/components/environmententry.md)[]                  | :heavy_check_mark:                                                                            | List of environment entries                                                                   |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | The ID of the environment                                                                     |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The name of the environment                                                                   |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | The organization ID this environment belongs to                                               |
| `projectId`                                                                                   | *string*                                                                                      | :heavy_check_mark:                                                                            | The project ID this environment belongs to                                                    |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | A short url-friendly label that uniquely identifies a resource.                               |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the environment was last updated                                                         |