# FilterOption

A single filter option (API key or user)

## Example Usage

```typescript
import { FilterOption } from "@gram/client/models/components/filteroption.js";

let value: FilterOption = {
  count: 838991,
  id: "<id>",
  label: "<value>",
};
```

## Fields

| Field                            | Type                             | Required                         | Description                      |
| -------------------------------- | -------------------------------- | -------------------------------- | -------------------------------- |
| `count`                          | *number*                         | :heavy_check_mark:               | Number of events for this option |
| `id`                             | *string*                         | :heavy_check_mark:               | Unique identifier for the option |
| `label`                          | *string*                         | :heavy_check_mark:               | Display label for the option     |