# TUMPeriod

## Example Usage

```typescript
import { TUMPeriod } from "@gram/client/models/components/tumperiod.js";

let value: TUMPeriod = {
  days: [],
  periodEnd: new Date("2025-11-09T15:50:17.577Z"),
  periodStart: new Date("2024-09-10T09:46:55.148Z"),
  tokens: 41831,
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `days`                                                                                        | [components.TUMPeriodDay](../../models/components/tumperiodday.md)[]                          | :heavy_check_mark:                                                                            | Daily breakdown of TUM within the cycle. Days without usage are omitted.                      |
| `periodEnd`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | End of the billing cycle (exclusive)                                                          |
| `periodStart`                                                                                 | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark:                                                                            | Start of the billing cycle                                                                    |
| `tokens`                                                                                      | *number*                                                                                      | :heavy_check_mark:                                                                            | Tokens under management consumed during the cycle                                             |