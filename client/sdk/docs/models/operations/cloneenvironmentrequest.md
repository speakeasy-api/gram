# CloneEnvironmentRequest

## Example Usage

```typescript
import { CloneEnvironmentRequest } from "@gram/client/models/operations/cloneenvironment.js";

let value: CloneEnvironmentRequest = {
  slug: "<value>",
  cloneEnvironmentRequestBody: {
    newName: "<value>",
  },
};
```

## Fields

| Field                                                                                            | Type                                                                                             | Required                                                                                         | Description                                                                                      |
| ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------ |
| `slug`                                                                                           | *string*                                                                                         | :heavy_check_mark:                                                                               | The slug of the source environment to clone                                                      |
| `gramSession`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | Session header                                                                                   |
| `gramProject`                                                                                    | *string*                                                                                         | :heavy_minus_sign:                                                                               | project header                                                                                   |
| `cloneEnvironmentRequestBody`                                                                    | [components.CloneEnvironmentRequestBody](../../models/components/cloneenvironmentrequestbody.md) | :heavy_check_mark:                                                                               | N/A                                                                                              |