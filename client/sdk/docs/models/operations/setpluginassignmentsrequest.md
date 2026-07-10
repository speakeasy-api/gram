# SetPluginAssignmentsRequest

## Example Usage

```typescript
import { SetPluginAssignmentsRequest } from "@gram/client/models/operations/setpluginassignments.js";

let value: SetPluginAssignmentsRequest = {
  setPluginAssignmentsForm: {
    pluginId: "f30ef195-a552-4077-9db1-ada3b341cb4c",
    principalUrns: [],
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `setPluginAssignmentsForm`                                                                 | [components.SetPluginAssignmentsForm](../../models/components/setpluginassignmentsform.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |