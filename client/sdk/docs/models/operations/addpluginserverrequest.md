# AddPluginServerRequest

## Example Usage

```typescript
import { AddPluginServerRequest } from "@gram/client/models/operations/addpluginserver.js";

let value: AddPluginServerRequest = {
  addPluginServerForm: {
    pluginId: "4c4a16a9-cdf1-4d3e-838c-e6b60430c94d",
  },
};
```

## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `gramSession`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | Session header                                                                   |
| `gramProject`                                                                    | *string*                                                                         | :heavy_minus_sign:                                                               | project header                                                                   |
| `addPluginServerForm`                                                            | [components.AddPluginServerForm](../../models/components/addpluginserverform.md) | :heavy_check_mark:                                                               | N/A                                                                              |