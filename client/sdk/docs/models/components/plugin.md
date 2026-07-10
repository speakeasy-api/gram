# Plugin

## Example Usage

```typescript
import { Plugin } from "@gram/client/models/components/plugin.js";

let value: Plugin = {
  createdAt: new Date("2024-03-16T07:24:27.625Z"),
  id: "301e0b94-cb20-40ff-a57b-a2aab65eee2f",
  name: "<value>",
  slug: "<value>",
  updatedAt: new Date("2025-08-20T23:14:50.852Z"),
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `assignmentCount`                                                                             | *number*                                                                                      | :heavy_minus_sign:                                                                            | Number of role/user assignments.                                                              |
| `assignments`                                                                                 | [components.PluginAssignment](../../models/components/pluginassignment.md)[]                  | :heavy_minus_sign:                                                                            | Role/user assignments.                                                                        |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `description`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Optional description.                                                                         |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Unique plugin identifier.                                                                     |
| `name`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | Display name.                                                                                 |
| `serverCount`                                                                                 | *number*                                                                                      | :heavy_minus_sign:                                                                            | Number of active servers in this plugin.                                                      |
| `servers`                                                                                     | [components.PluginServer](../../models/components/pluginserver.md)[]                          | :heavy_minus_sign:                                                                            | Servers included in this plugin.                                                              |
| `slug`                                                                                        | *string*                                                                                      | :heavy_check_mark:                                                                            | URL-safe identifier, unique per org.                                                          |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |