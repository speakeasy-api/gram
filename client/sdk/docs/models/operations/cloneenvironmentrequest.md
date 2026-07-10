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

| Field                         | Type                                                                                             | Required           | Description                                 |
| ----------------------------- | ------------------------------------------------------------------------------------------------ | ------------------ | ------------------------------------------- |
| `slug`                        | _string_                                                                                         | :heavy_check_mark: | The slug of the source environment to clone |
| `gramSession`                 | _string_                                                                                         | :heavy_minus_sign: | Session header                              |
| `gramProject`                 | _string_                                                                                         | :heavy_minus_sign: | project header                              |
| `cloneEnvironmentRequestBody` | [components.CloneEnvironmentRequestBody](../../models/components/cloneenvironmentrequestbody.md) | :heavy_check_mark: | N/A                                         |
