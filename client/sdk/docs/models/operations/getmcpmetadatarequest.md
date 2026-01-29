# GetMcpMetadataRequest

## Example Usage

```typescript
import { GetMcpMetadataRequest } from "@gram/client/models/operations";

let value: GetMcpMetadataRequest = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                                              | Type                                                               | Required                                                           | Description                                                        |
| ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ | ------------------------------------------------------------------ |
| `toolsetSlug`                                                      | *string*                                                           | :heavy_check_mark:                                                 | The slug of the toolset associated with this install page metadata |
| `gramKey`                                                          | *string*                                                           | :heavy_minus_sign:                                                 | API Key header                                                     |
| `gramSession`                                                      | *string*                                                           | :heavy_minus_sign:                                                 | Session header                                                     |
| `gramProject`                                                      | *string*                                                           | :heavy_minus_sign:                                                 | project header                                                     |