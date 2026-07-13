# TumDetailsBreakdownRow

One value of a breakdown dimension with its token usage over the range

## Example Usage

```typescript
import { TumDetailsBreakdownRow } from "@gram/client/models/components/tumdetailsbreakdownrow.js";

let value: TumDetailsBreakdownRow = {
  series: [],
  totalTokens: 370943,
  value: "<value>",
};
```

## Fields

| Field                                                                                             | Type                                                                                              | Required                                                                                          | Description                                                                                       |
| ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| `series`                                                                                          | *number*[]                                                                                        | :heavy_check_mark:                                                                                | Daily tokens aligned to the result's points buckets                                               |
| `totalTokens`                                                                                     | *number*                                                                                          | :heavy_check_mark:                                                                                | Tokens for this value over the range                                                              |
| `value`                                                                                           | *string*                                                                                          | :heavy_check_mark:                                                                                | The dimension value; empty for rows without the attribute, 'Other' for the top-N remainder rollup |