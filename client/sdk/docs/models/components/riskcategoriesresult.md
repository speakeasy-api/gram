# RiskCategoriesResult

Canonical risk category definitions used to classify findings, in classification-priority order. Consumers should iterate in order and pick the first match.

## Example Usage

```typescript
import { RiskCategoriesResult } from "@gram/client/models/components/riskcategoriesresult.js";

let value: RiskCategoriesResult = {
  categories: [
    {
      description: "yum incidentally after discourse annually",
      icon: "<value>",
      key: "<key>",
      label: "<value>",
      ruleIdPrefix: "<value>",
      ruleIds: [],
      source: "<value>",
    },
  ],
};
```

## Fields

| Field        | Type                                                                                     | Required           | Description                                                                                                                      |
| ------------ | ---------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------- |
| `categories` | [components.RiskCategoryDefinition](../../models/components/riskcategorydefinition.md)[] | :heavy_check_mark: | Categories in classification-priority order. The last entry is the 'custom' fallback for findings that match none of the others. |
