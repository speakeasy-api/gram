# TumDetailsBreakdown

Per-dimension token breakdown for the usage details table

## Example Usage

```typescript
import { TumDetailsBreakdown } from "@gram/client/models/components/tumdetailsbreakdown.js";

let value: TumDetailsBreakdown = {
  key: "<key>",
  rows: [],
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `key`                                                                                    | *string*                                                                                 | :heavy_check_mark:                                                                       | The breakdown dimension key (matches telemetry.query group_by)                           |
| `rows`                                                                                   | [components.TumDetailsBreakdownRow](../../models/components/tumdetailsbreakdownrow.md)[] | :heavy_check_mark:                                                                       | Top values by tokens in descending order, with the remainder rolled into 'Other'         |