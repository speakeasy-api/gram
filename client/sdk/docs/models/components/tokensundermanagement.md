# TokensUnderManagement

## Example Usage

```typescript
import { TokensUnderManagement } from "@gram/client/models/components/tokensundermanagement.js";

let value: TokensUnderManagement = {
  billingCycleAnchorDay: 928417,
  history: [],
  periodEnd: new Date("2025-11-24T00:34:59.738Z"),
  periodStart: new Date("2025-12-16T07:20:43.047Z"),
  tokens: 369557,
};
```

## Fields

| Field                    | Type                                                                                          | Required           | Description                                                                                            |
| ------------------------ | --------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------------------------------------------------ |
| `alertEmail`             | _string_                                                                                      | :heavy_minus_sign: | Email address to notify on TUM threshold events. Only populated for platform admins.                   |
| `billingCycleAnchorDay`  | _number_                                                                                      | :heavy_check_mark: | Day of month (1-31) the billing cycle starts, at 00:00 UTC                                             |
| `history`                | [components.TUMPeriod](../../models/components/tumperiod.md)[]                                | :heavy_check_mark: | TUM usage per billing cycle for the trailing cycles, oldest first. The last entry is the active cycle. |
| `monthlyTokenLimit`      | _number_                                                                                      | :heavy_minus_sign: | The contracted monthly tokens under management limit, if one has been configured                       |
| `periodEnd`              | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | End of the active billing cycle (exclusive)                                                            |
| `periodStart`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | Start of the active billing cycle                                                                      |
| `tokens`                 | _number_                                                                                      | :heavy_check_mark: | Tokens under management consumed during the active billing cycle                                       |
| `tunneledMcpServerLimit` | _number_                                                                                      | :heavy_minus_sign: | The contracted tunneled MCP server source cap, if one has been configured                              |
