# RemovePluginServerRequest

## Example Usage

```typescript
import { RemovePluginServerRequest } from "@gram/client/models/operations/removepluginserver.js";

let value: RemovePluginServerRequest = {
  id: "98004287-7234-4db0-989e-627e1f669130",
  pluginId: "d1bb1712-f80a-42f7-9dfb-9ae1b9bd5363",
};
```

## Fields

| Field                           | Type                            | Required                        | Description                     |
| ------------------------------- | ------------------------------- | ------------------------------- | ------------------------------- |
| `id`                            | *string*                        | :heavy_check_mark:              | The plugin server ID to remove. |
| `pluginId`                      | *string*                        | :heavy_check_mark:              | N/A                             |
| `gramSession`                   | *string*                        | :heavy_minus_sign:              | Session header                  |
| `gramProject`                   | *string*                        | :heavy_minus_sign:              | project header                  |