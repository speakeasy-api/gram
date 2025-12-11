# SetProjectLogoRequest

## Example Usage

```typescript
import { SetProjectLogoRequest } from "@gram/client/models/operations";

let value: SetProjectLogoRequest = {
  getSignedAssetURLForm: {
    assetId: "<id>",
  },
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `gramKey`                                                                            | *string*                                                                             | :heavy_minus_sign:                                                                   | API Key header                                                                       |
| `gramSession`                                                                        | *string*                                                                             | :heavy_minus_sign:                                                                   | Session header                                                                       |
| `gramProject`                                                                        | *string*                                                                             | :heavy_minus_sign:                                                                   | project header                                                                       |
| `getSignedAssetURLForm`                                                              | [components.GetSignedAssetURLForm](../../models/components/getsignedasseturlform.md) | :heavy_check_mark:                                                                   | N/A                                                                                  |