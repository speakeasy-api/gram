# PublishPackageForm

## Example Usage

```typescript
import { PublishPackageForm } from "@gram/client/models/components";

let value: PublishPackageForm = {
  deploymentId: "<id>",
  name: "<value>",
  version: "<value>",
  visibility: "private",
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `deploymentId`                                                 | *string*                                                       | :heavy_check_mark:                                             | The deployment ID to associate with the package version        |
| `name`                                                         | *string*                                                       | :heavy_check_mark:                                             | The name of the package                                        |
| `version`                                                      | *string*                                                       | :heavy_check_mark:                                             | The new semantic version of the package to publish             |
| `visibility`                                                   | [components.Visibility](../../models/components/visibility.md) | :heavy_check_mark:                                             | The visibility of the package version                          |