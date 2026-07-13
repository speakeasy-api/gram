# GetRiskUserBreakdownRequest

## Example Usage

```typescript
import { GetRiskUserBreakdownRequest } from "@gram/client/models/operations/getriskuserbreakdown.js";

let value: GetRiskUserBreakdownRequest = {
  externalUserId: "<id>",
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                                       |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------------------------------------------------- |
| `externalUserId` | _string_                                                                                      | :heavy_check_mark: | External user identifier to scope the breakdown to.                               |
| `from`           | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Inclusive start of the window. Defaults to the same 7-day window as the overview. |
| `to`             | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Exclusive end of the window. Defaults to now.                                     |
| `gramKey`        | _string_                                                                                      | :heavy_minus_sign: | API Key header                                                                    |
| `gramSession`    | _string_                                                                                      | :heavy_minus_sign: | Session header                                                                    |
| `gramProject`    | _string_                                                                                      | :heavy_minus_sign: | project header                                                                    |
