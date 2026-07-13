# DeleteGlobalVariationRequest

## Example Usage

```typescript
import { DeleteGlobalVariationRequest } from "@gram/client/models/operations/deleteglobalvariation.js";

let value: DeleteGlobalVariationRequest = {
  variationId: "<id>",
};
```

## Fields

| Field         | Type     | Required           | Description                       |
| ------------- | -------- | ------------------ | --------------------------------- |
| `variationId` | _string_ | :heavy_check_mark: | The ID of the variation to delete |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                    |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                    |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                    |
