# SetSourceEnvironmentLinkRequest

## Example Usage

```typescript
import { SetSourceEnvironmentLinkRequest } from "@gram/client/models/operations";

let value: SetSourceEnvironmentLinkRequest = {
  setSourceEnvironmentLinkRequestBody: {
    environmentId: "59aa3cb1-eb15-4d06-86cb-ca3127f8703c",
    sourceKind: "function",
    sourceSlug: "<value>",
  },
};
```

## Fields

| Field                                                                                                            | Type                                                                                                             | Required                                                                                                         | Description                                                                                                      |
| ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| `gramSession`                                                                                                    | *string*                                                                                                         | :heavy_minus_sign:                                                                                               | Session header                                                                                                   |
| `gramProject`                                                                                                    | *string*                                                                                                         | :heavy_minus_sign:                                                                                               | project header                                                                                                   |
| `setSourceEnvironmentLinkRequestBody`                                                                            | [components.SetSourceEnvironmentLinkRequestBody](../../models/components/setsourceenvironmentlinkrequestbody.md) | :heavy_check_mark:                                                                                               | N/A                                                                                                              |