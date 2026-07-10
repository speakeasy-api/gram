# ServerNameOverride

User-defined display name for a hooks server

## Example Usage

```typescript
import { ServerNameOverride } from "@gram/client/models/components/servernameoverride.js";

let value: ServerNameOverride = {
  displayName: "Colby_Corwin",
  id: "<id>",
  rawServerName: "<value>",
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `displayName`                   | *string*                        | :heavy_check_mark:              | User-friendly display name      |
| `id`                            | *string*                        | :heavy_check_mark:              | Override ID                     |
| `rawServerName`                 | *string*                        | :heavy_check_mark:              | Original server name from hooks |