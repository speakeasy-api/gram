# SetProjectLogoRequest

## Example Usage

```typescript
import { SetProjectLogoRequest } from "@gram/client/models/operations";

let value: SetProjectLogoRequest = {
  setProjectLogoForm: {
    assetId: "<id>",
  },
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `gramKey`                                                                      | *string*                                                                       | :heavy_minus_sign:                                                             | API Key header                                                                 |
| `gramSession`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | Session header                                                                 |
| `gramProject`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | project header                                                                 |
| `setProjectLogoForm`                                                           | [components.SetProjectLogoForm](../../models/components/setprojectlogoform.md) | :heavy_check_mark:                                                             | N/A                                                                            |