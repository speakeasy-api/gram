# GetRiskUserBreakdownRequest

## Example Usage

```typescript
import { GetRiskUserBreakdownRequest } from "@gram/client/models/operations/getriskuserbreakdown.js";

let value: GetRiskUserBreakdownRequest = {
  externalUserId: "<id>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `externalUserId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | External user identifier to scope the breakdown to.                                           |
| `from`                                                                                        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Inclusive start of the window. Defaults to the same 7-day window as the overview.             |
| `to`                                                                                          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | Exclusive end of the window. Defaults to now.                                                 |
| `gramKey`                                                                                     | *string*                                                                                      | :heavy_minus_sign:                                                                            | API Key header                                                                                |
| `gramSession`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | Session header                                                                                |
| `gramProject`                                                                                 | *string*                                                                                      | :heavy_minus_sign:                                                                            | project header                                                                                |