# ListPluginsResult

## Example Usage

```typescript
import { ListPluginsResult } from "@gram/client/models/components/listpluginsresult.js";

let value: ListPluginsResult = {
  plugins: [
    {
      createdAt: new Date("2024-09-29T03:29:08.226Z"),
      id: "5afe5e6e-a155-4043-92d4-d4a36d2231bd",
      name: "<value>",
      slug: "<value>",
      updatedAt: new Date("2025-02-25T17:42:36.003Z"),
    },
  ],
};
```

## Fields

| Field     | Type                                                     | Required           | Description                      |
| --------- | -------------------------------------------------------- | ------------------ | -------------------------------- |
| `plugins` | [components.Plugin](../../models/components/plugin.md)[] | :heavy_check_mark: | The plugins in the organization. |
