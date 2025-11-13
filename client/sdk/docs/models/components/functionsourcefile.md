# FunctionSourceFile

## Example Usage

```typescript
import { FunctionSourceFile } from "@gram/client/models/components";

let value: FunctionSourceFile = {
  content: "<value>",
  path: "/var/tmp",
  size: 304074,
};
```

## Fields

| Field                                            | Type                                             | Required                                         | Description                                      |
| ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ | ------------------------------------------------ |
| `content`                                        | *string*                                         | :heavy_check_mark:                               | The content of the file                          |
| `isBinary`                                       | *boolean*                                        | :heavy_minus_sign:                               | Whether the file is binary (non-text)            |
| `path`                                           | *string*                                         | :heavy_check_mark:                               | The relative path of the file within the archive |
| `size`                                           | *number*                                         | :heavy_check_mark:                               | The size of the file in bytes                    |