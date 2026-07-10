# CreatePluginRequest

## Example Usage

```typescript
import { CreatePluginRequest } from "@gram/client/models/operations/createplugin.js";

let value: CreatePluginRequest = {
  createPluginForm: {
    name: "<value>",
  },
};
```

## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `gramSession`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | Session header                                                             |
| `gramProject`                                                              | *string*                                                                   | :heavy_minus_sign:                                                         | project header                                                             |
| `createPluginForm`                                                         | [components.CreatePluginForm](../../models/components/createpluginform.md) | :heavy_check_mark:                                                         | N/A                                                                        |