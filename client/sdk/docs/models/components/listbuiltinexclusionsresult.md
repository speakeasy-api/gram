# ListBuiltinExclusionsResult

## Example Usage

```typescript
import { ListBuiltinExclusionsResult } from "@gram/client/models/components/listbuiltinexclusionsresult.js";

let value: ListBuiltinExclusionsResult = {
  categories: [],
  version: "<value>",
};
```

## Fields

| Field        | Type                                                                                         | Required           | Description                               |
| ------------ | -------------------------------------------------------------------------------------------- | ------------------ | ----------------------------------------- |
| `categories` | [components.BuiltinExclusionCategory](../../models/components/builtinexclusioncategory.md)[] | :heavy_check_mark: | The library grouped by category.          |
| `version`    | _string_                                                                                     | :heavy_check_mark: | Catalog checksum/version, for provenance. |
