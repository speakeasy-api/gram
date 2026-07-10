# BuiltinExclusionCategory

A named group of built-in exclusion rules.

## Example Usage

```typescript
import { BuiltinExclusionCategory } from "@gram/client/models/components/builtinexclusioncategory.js";

let value: BuiltinExclusionCategory = {
  entries: [
    {
      description: "annex joy galoshes essay afore of ew about",
      id: "<id>",
      reason: "<value>",
    },
  ],
  label: "<value>",
};
```

## Fields

| Field                                                                                  | Type                                                                                   | Required                                                                               | Description                                                                            |
| -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `entries`                                                                              | [components.BuiltinExclusionEntry](../../models/components/builtinexclusionentry.md)[] | :heavy_check_mark:                                                                     | The rules in this category.                                                            |
| `label`                                                                                | *string*                                                                               | :heavy_check_mark:                                                                     | Human category label, e.g. "Test credit cards".                                        |