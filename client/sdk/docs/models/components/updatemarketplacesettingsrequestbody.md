# UpdateMarketplaceSettingsRequestBody

## Example Usage

```typescript
import { UpdateMarketplaceSettingsRequestBody } from "@gram/client/models/components/updatemarketplacesettingsrequestbody.js";

let value: UpdateMarketplaceSettingsRequestBody = {};
```

## Fields

| Field             | Type     | Required           | Description                                                                                                                                                                 |
| ----------------- | -------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `marketplaceName` | _string_ | :heavy_minus_sign: | Override for the marketplace name (the identifier users type as `<plugin>@<marketplace>`). Pass an empty string or omit to clear the override and fall back to the default. |
