# AddFunctionsForm

## Example Usage

```typescript
import { AddFunctionsForm } from "@gram/client/models/components/addfunctionsform.js";

let value: AddFunctionsForm = {
  assetId: "<id>",
  name: "<value>",
  runtime: "<value>",
  slug: "<value>",
};
```

## Fields

| Field       | Type     | Required           | Description                                                                                         |
| ----------- | -------- | ------------------ | --------------------------------------------------------------------------------------------------- |
| `assetId`   | _string_ | :heavy_check_mark: | The ID of the functions file from the assets service.                                               |
| `memoryMib` | _number_ | :heavy_minus_sign: | The amount of memory in MiB to allocate for the function (1 MiB = 1024 \* 1024 bytes).              |
| `name`      | _string_ | :heavy_check_mark: | The functions file display name.                                                                    |
| `runtime`   | _string_ | :heavy_check_mark: | The runtime to use when executing functions. Allowed values are: nodejs:22, nodejs:24, python:3.12. |
| `scale`     | _number_ | :heavy_minus_sign: | The number of instances to scale the function to.                                                   |
| `slug`      | _string_ | :heavy_check_mark: | A short url-friendly label that uniquely identifies a resource.                                     |
