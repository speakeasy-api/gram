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

| Field   | Type     | Required           | Description                      |
| ------- | -------- | ------------------ | -------------------------------- |
| `count` | _number_ | :heavy_check_mark: | Number of events for this option |
| `id`    | _string_ | :heavy_check_mark: | Unique identifier for the option |
| `label` | _string_ | :heavy_check_mark: | Display label for the option     |
