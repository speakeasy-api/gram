# MarketplaceSettingsResult

## Example Usage

```typescript
import { MarketplaceSettingsResult } from "@gram/client/models/components/marketplacesettingsresult.js";

let value: MarketplaceSettingsResult = {
  defaultName: "<value>",
  effectiveName: "<value>",
};
```

## Fields

| Field                                                                                        | Type                                                                                         | Required                                                                                     | Description                                                                                  |
| -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------- |
| `defaultName`                                                                                | *string*                                                                                     | :heavy_check_mark:                                                                           | The default marketplace name used when no override is configured.                            |
| `effectiveName`                                                                              | *string*                                                                                     | :heavy_check_mark:                                                                           | The marketplace name that will be used at publish time (override if set, otherwise default). |
| `marketplaceName`                                                                            | *string*                                                                                     | :heavy_minus_sign:                                                                           | User-provided override for the marketplace name. Absent when no override is configured.      |