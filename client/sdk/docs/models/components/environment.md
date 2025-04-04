# Environment

Model representing an environment

## Example Usage

```typescript
import { Environment } from "@gram/sdk/models/components";

let value: Environment = {
  createdAt: new Date("2023-05-10T15:05:25.793Z"),
  entries: [
    {
      createdAt: new Date("2024-12-02T08:28:57.162Z"),
      name: "<value>",
      updatedAt: new Date("2023-06-07T02:45:53.539Z"),
      value: "<value>",
    },
  ],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  projectId: "<id>",
  slug: "<value>",
  updatedAt: new Date("2025-11-01T08:34:16.299Z"),
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
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | The slug identifier for the environment                                                       |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | When the environment was last updated                                                         |