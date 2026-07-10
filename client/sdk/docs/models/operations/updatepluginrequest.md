# UpdatePluginRequest

## Example Usage

```typescript
import { UpdatePluginRequest } from "@gram/client/models/operations/updateplugin.js";

let value: UpdatePluginRequest = {
  updatePluginForm: {
    id: "fe2c4f06-7313-43a7-b170-9eb08b6827d0",
    name: "<value>",
    slug: "<value>",
  },
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `gramSession`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | Session header                                                             |
| `gramProject`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | project header                                                             |
| `updatePluginForm`                                                         | [components.UpdatePluginForm](../../models/components/updatepluginform.md) | :heavy_check_mark:                                                         | N/A                                                                        |