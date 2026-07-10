# TUMPeriodDay

## Example Usage

```typescript
import { TUMPeriodDay } from "@gram/client/models/components/tumperiodday.js";
import { RFCDate } from "@gram/client/types/rfcdate.js";

let value: TUMPeriodDay = {
  date: new RFCDate("2025-03-01"),
  tokens: 698199,
};
```

## Fields

| Field                                        | Type                                         | Required                                     | Description                                  |
| -------------------------------------------- | -------------------------------------------- | -------------------------------------------- | -------------------------------------------- |
| `date`                                       | [RFCDate](../../types/rfcdate.md)            | :heavy_check_mark:                           | The UTC day                                  |
| `tokens`                                     | *number*                                     | :heavy_check_mark:                           | Tokens under management consumed on this day |