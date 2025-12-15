# UpsertAllowedOriginForm

## Example Usage

```typescript
import { UpsertAllowedOriginForm } from "@gram/client/models/components";

let value: UpsertAllowedOriginForm = {
  origin: "<value>",
};
```

## Fields

| Field                                                    | Type                                                     | Required                                                 | Description                                              |
| -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- | -------------------------------------------------------- |
| `origin`                                                 | *string*                                                 | :heavy_check_mark:                                       | The origin URL to upsert                                 |
| `status`                                                 | *string*                                                 | :heavy_minus_sign:                                       | The status of the allowed origin (defaults to 'pending') |