# CreateReleaseRequest

## Example Usage

```typescript
import { CreateReleaseRequest } from "@gram/client/models/operations";

let value: CreateReleaseRequest = {
  createReleaseRequestBody: {
    toolsetSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `gramKey`                                                                                  | *string*                                                                                   | :heavy_minus_sign:                                                                         | API Key header                                                                             |
| `gramProject`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | project header                                                                             |
| `createReleaseRequestBody`                                                                 | [components.CreateReleaseRequestBody](../../models/components/createreleaserequestbody.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |