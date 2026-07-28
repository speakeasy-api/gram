# UpdateEnvironmentRequest

## Example Usage

```typescript
import { UpdateEnvironmentRequest } from "@gram/client/models/operations/updateenvironment.js";

let value: UpdateEnvironmentRequest = {
  slug: "<value>",
  updateEnvironmentRequestBody: {
    entriesToRemove: ["<value 1>", "<value 2>", "<value 3>"],
    entriesToUpdate: [],
  },
};
```

## Fields

| Field                          | Type                                                                                               | Required           | Description                           |
| ------------------------------ | -------------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------- |
| `slug`                         | _string_                                                                                           | :heavy_check_mark: | The slug of the environment to update |
| `gramSession`                  | _string_                                                                                           | :heavy_minus_sign: | Session header                        |
| `gramProject`                  | _string_                                                                                           | :heavy_minus_sign: | project header                        |
| `updateEnvironmentRequestBody` | [components.UpdateEnvironmentRequestBody](../../models/components/updateenvironmentrequestbody.md) | :heavy_check_mark: | N/A                                   |
