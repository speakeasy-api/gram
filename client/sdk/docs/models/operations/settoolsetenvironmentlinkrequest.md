# SetToolsetEnvironmentLinkRequest

## Example Usage

```typescript
import { SetToolsetEnvironmentLinkRequest } from "@gram/client/models/operations";

let value: SetToolsetEnvironmentLinkRequest = {
  setToolsetEnvironmentLinkRequestBody: {
    environmentId: "401c4a62-2be0-404d-b2c5-70b079d7dc4f",
    toolsetId: "eb229757-ee1d-42e5-96ae-d0d662cb4f56",
  },
};
```

## Fields

| Field                                                                                                              | Type                                                                                                               | Required                                                                                                           | Description                                                                                                        |
| ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------ |
| `gramSession`                                                                                                      | *string*                                                                                                           | :heavy_minus_sign:                                                                                                 | Session header                                                                                                     |
| `gramProject`                                                                                                      | *string*                                                                                                           | :heavy_minus_sign:                                                                                                 | project header                                                                                                     |
| `setToolsetEnvironmentLinkRequestBody`                                                                             | [components.SetToolsetEnvironmentLinkRequestBody](../../models/components/settoolsetenvironmentlinkrequestbody.md) | :heavy_check_mark:                                                                                                 | N/A                                                                                                                |