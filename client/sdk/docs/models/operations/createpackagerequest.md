# CreatePackageRequest

## Example Usage

```typescript
import { CreatePackageRequest } from "@gram/client/models/operations/createpackage.js";

let value: CreatePackageRequest = {
  createPackageForm: {
    name: "<value>",
    summary: "<value>",
    title: "<value>",
  },
};
```

## Fields

| Field               | Type                                                                         | Required           | Description    |
| ------------------- | ---------------------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`           | _string_                                                                     | :heavy_minus_sign: | API Key header |
| `gramSession`       | _string_                                                                     | :heavy_minus_sign: | Session header |
| `gramProject`       | _string_                                                                     | :heavy_minus_sign: | project header |
| `createPackageForm` | [components.CreatePackageForm](../../models/components/createpackageform.md) | :heavy_check_mark: | N/A            |
