# AddPackageForm

## Example Usage

```typescript
import { AddPackageForm } from "@gram/client/models/components";

let value: AddPackageForm = {
  name: "<value>",
};
```

## Fields

| Field                                                                           | Type                                                                            | Required                                                                        | Description                                                                     |
| ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- | ------------------------------------------------------------------------------- |
| `name`                                                                          | *string*                                                                        | :heavy_check_mark:                                                              | The name of the package to add.                                                 |
| `version`                                                                       | *string*                                                                        | :heavy_minus_sign:                                                              | The version of the package to add. If omitted, the latest version will be used. |