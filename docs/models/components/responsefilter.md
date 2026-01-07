# ResponseFilter

Response filter metadata for the tool

## Example Usage

```typescript
import { ResponseFilter } from "@gram/client/models/components";

let value: ResponseFilter = {
  contentTypes: [
    "<value 1>",
    "<value 2>",
  ],
  statusCodes: [
    "<value 1>",
    "<value 2>",
  ],
  type: "<value>",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `contentTypes`                    | *string*[]                        | :heavy_check_mark:                | Content types to filter for       |
| `statusCodes`                     | *string*[]                        | :heavy_check_mark:                | Status codes to filter for        |
| `type`                            | *string*                          | :heavy_check_mark:                | Response filter type for the tool |