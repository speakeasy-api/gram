# ListResourcesResult

## Example Usage

```typescript
import { ListResourcesResult } from "@gram/client/models/components";

let value: ListResourcesResult = {
  resources: [
    {},
  ],
};
```

## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `nextCursor`                                                 | *string*                                                     | :heavy_minus_sign:                                           | The cursor to fetch results from                             |
| `resources`                                                  | [components.Resource](../../models/components/resource.md)[] | :heavy_check_mark:                                           | The list of resources                                        |