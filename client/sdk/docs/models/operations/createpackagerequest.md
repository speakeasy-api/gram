# CreatePackageRequest

## Example Usage

```typescript
import { CreatePackageRequest } from "@gram/client/models/operations";

let value: CreatePackageRequest = {
  createPackageForm: {
    name: "<value>",
    summary: "<value>",
    title: "<value>",
  },
};
```

## Fields

| Field                                                                        | Type                                                                         | Required                                                                     | Description                                                                  |
| ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| `gramKey`                                                                    | *string*                                                                     | :heavy_minus_sign:                                                           | API Key header                                                               |
| `gramSession`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | Session header                                                               |
| `gramProject`                                                                | *string*                                                                     | :heavy_minus_sign:                                                           | project header                                                               |
| `createPackageForm`                                                          | [components.CreatePackageForm](../../models/components/createpackageform.md) | :heavy_check_mark:                                                           | N/A                                                                          |