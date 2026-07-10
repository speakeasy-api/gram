# GetRiskOverviewRequest

## Example Usage

```typescript
import { GetRiskOverviewRequest } from "@gram/client/models/operations/getriskoverview.js";

let value: GetRiskOverviewRequest = {};
```

## Fields

| Field         | Type                                                                                          | Required           | Description                                                                                              |
| ------------- | --------------------------------------------------------------------------------------------- | ------------------ | -------------------------------------------------------------------------------------------------------- |
| `from`        | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Inclusive start of the overview window. Defaults to the start of the 7-day calendar window ending at to. |
| `to`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign: | Exclusive end of the overview window. Defaults to now.                                                   |
| `gramKey`     | _string_                                                                                      | :heavy_minus_sign: | API Key header                                                                                           |
| `gramSession` | _string_                                                                                      | :heavy_minus_sign: | Session header                                                                                           |
| `gramProject` | _string_                                                                                      | :heavy_minus_sign: | project header                                                                                           |
