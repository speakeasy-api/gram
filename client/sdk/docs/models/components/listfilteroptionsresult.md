# ListFilterOptionsResult

Result of listing filter options

## Example Usage

```typescript
import { ListFilterOptionsResult } from "@gram/client/models/components/listfilteroptionsresult.js";

let value: ListFilterOptionsResult = {
  options: [
    {
      count: 304560,
      id: "<id>",
      label: "<value>",
    },
  ],
};
```

## Fields

| Field     | Type                                                                 | Required           | Description            |
| --------- | -------------------------------------------------------------------- | ------------------ | ---------------------- |
| `options` | [components.FilterOption](../../models/components/filteroption.md)[] | :heavy_check_mark: | List of filter options |
