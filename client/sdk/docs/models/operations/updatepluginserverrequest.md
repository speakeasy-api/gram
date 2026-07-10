# UpdatePluginServerRequest

## Example Usage

```typescript
import { UpdatePluginServerRequest } from "@gram/client/models/operations/updatepluginserver.js";

let value: UpdatePluginServerRequest = {
  updatePluginServerForm: {
    displayName: "Koby8",
    id: "b4016c7c-7724-4234-9f31-6d5d9dbfa48c",
    pluginId: "69fc4ee5-8543-4b61-944f-da2a75ec9c41",
  },
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `gramSession`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | Session header                                                                         |
| `gramProject`                                                                          | *string*                                                                               | :heavy_minus_sign:                                                                     | project header                                                                         |
| `updatePluginServerForm`                                                               | [components.UpdatePluginServerForm](../../models/components/updatepluginserverform.md) | :heavy_check_mark:                                                                     | N/A                                                                                    |