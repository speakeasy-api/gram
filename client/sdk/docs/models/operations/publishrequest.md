# PublishRequest

## Example Usage

```typescript
import { PublishRequest } from "@gram/client/models/operations";

let value: PublishRequest = {
  publishPackageForm: {
    deploymentId: "<id>",
    name: "<value>",
    version: "<value>",
    visibility: "public",
  },
};
```

## Fields

| Field                                                                          | Type                                                                           | Required                                                                       | Description                                                                    |
| ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ | ------------------------------------------------------------------------------ |
| `gramKey`                                                                      | *string*                                                                       | :heavy_minus_sign:                                                             | API Key header                                                                 |
| `gramSession`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | Session header                                                                 |
| `gramProject`                                                                  | *string*                                                                       | :heavy_minus_sign:                                                             | project header                                                                 |
| `publishPackageForm`                                                           | [components.PublishPackageForm](../../models/components/publishpackageform.md) | :heavy_check_mark:                                                             | N/A                                                                            |