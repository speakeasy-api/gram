# QueryFilter

A single filter predicate on an allowlisted dimension

## Example Usage

```typescript
import { QueryFilter } from "@gram/client/models/components/queryfilter.js";

let value: QueryFilter = {
  dimension: "hook_source",
  values: [
    "<value 1>",
    "<value 2>",
    "<value 3>",
  ],
};
```

## Fields

| Field                                                                                                                                             | Type                                                                                                                                              | Required                                                                                                                                          | Description                                                                                                                                       |
| ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------- |
| `dimension`                                                                                                                                       | [components.Dimension](../../models/components/dimension.md)                                                                                      | :heavy_check_mark:                                                                                                                                | Dimension to filter on                                                                                                                            |
| `values`                                                                                                                                          | *string*[]                                                                                                                                        | :heavy_check_mark:                                                                                                                                | Match if the dimension equals any of these values (IN semantics; for multi-valued dimensions like role/group, matches if any element is present). |