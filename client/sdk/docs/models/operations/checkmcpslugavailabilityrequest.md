# CheckMCPSlugAvailabilityRequest

## Example Usage

```typescript
import { CheckMCPSlugAvailabilityRequest } from "@gram/client/models/operations/checkmcpslugavailability.js";

let value: CheckMCPSlugAvailabilityRequest = {
  slug: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description       |
| ------------- | -------- | ------------------ | ----------------- |
| `slug`        | _string_ | :heavy_check_mark: | The slug to check |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header    |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header    |
| `gramProject` | _string_ | :heavy_minus_sign: | project header    |
