# PluginAssignment

## Example Usage

```typescript
import { PluginAssignment } from "@gram/client/models/components/pluginassignment.js";

let value: PluginAssignment = {
  createdAt: new Date("2026-09-26T09:39:31.412Z"),
  id: "567d8212-4d76-4c38-9ab5-a3d0de541e06",
  principalUrn: "<value>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | N/A                                                                                           |
| `id`                                                                                          | *string*                                                                                      | :heavy_check_mark:                                                                            | Unique assignment identifier.                                                                 |
| `principalUrn`                                                                                | *string*                                                                                      | :heavy_check_mark:                                                                            | Principal URN (e.g. role:engineering, user:id, or *).                                         |