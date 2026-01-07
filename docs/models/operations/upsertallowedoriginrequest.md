# UpsertAllowedOriginRequest

## Example Usage

```typescript
import { UpsertAllowedOriginRequest } from "@gram/client/models/operations";

let value: UpsertAllowedOriginRequest = {
  upsertAllowedOriginForm: {
    origin: "<value>",
  },
};
```

## Fields

| Field                                                                                    | Type                                                                                     | Required                                                                                 | Description                                                                              |
| ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `gramKey`                                                                                | *string*                                                                                 | :heavy_minus_sign:                                                                       | API Key header                                                                           |
| `gramSession`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | Session header                                                                           |
| `gramProject`                                                                            | *string*                                                                                 | :heavy_minus_sign:                                                                       | project header                                                                           |
| `upsertAllowedOriginForm`                                                                | [components.UpsertAllowedOriginForm](../../models/components/upsertallowedoriginform.md) | :heavy_check_mark:                                                                       | N/A                                                                                      |