# SetProjectLogoRequest

## Example Usage

```typescript
import { SetProjectLogoRequest } from "@gram/client/models/operations/setprojectlogo.js";

let value: SetProjectLogoRequest = {
  getSignedAssetURLForm: {
    assetId: "<id>",
  },
};
```

## Fields

| Field                   | Type                                                                                 | Required           | Description    |
| ----------------------- | ------------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`               | _string_                                                                             | :heavy_minus_sign: | API Key header |
| `gramSession`           | _string_                                                                             | :heavy_minus_sign: | Session header |
| `gramProject`           | _string_                                                                             | :heavy_minus_sign: | project header |
| `getSignedAssetURLForm` | [components.GetSignedAssetURLForm](../../models/components/getsignedasseturlform.md) | :heavy_check_mark: | N/A            |
