# AddFunctionsForm

## Example Usage

```typescript
import { AddFunctionsForm } from "@gram/client/models/components";

let value: AddFunctionsForm = {
  assetId: "<id>",
  name: "<value>",
  runtime: "<value>",
  slug: "<value>",
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `assetId`                                                                                | *string*                                                                                 | :heavy_check_mark:                                                                       | The ID of the functions file from the assets service.                                    |
| `name`                                                                                   | *string*                                                                                 | :heavy_check_mark:                                                                       | The functions file display name.                                                         |
| `runtime`                                                                                | *string*                                                                                 | :heavy_check_mark:                                                                       | The runtime to use when executing functions. Allowed values are: nodejs:22, python:3.12. |
| `slug`                                                                                   | *string*                                                                                 | :heavy_check_mark:                                                                       | A short url-friendly label that uniquely identifies a resource.                          |