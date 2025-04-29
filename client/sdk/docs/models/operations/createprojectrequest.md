# CreateProjectRequest

## Example Usage

```typescript
import { CreateProjectRequest } from "@gram/client/models/operations";

let value: CreateProjectRequest = {
  createProjectRequestBody: {
    name: "<value>",
    organizationId: "<id>",
  },
};
```

## Fields

| Field                                                                                      | Type                                                                                       | Required                                                                                   | Description                                                                                |
| ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------ |
| `gramSession`                                                                              | *string*                                                                                   | :heavy_minus_sign:                                                                         | Session header                                                                             |
| `createProjectRequestBody`                                                                 | [components.CreateProjectRequestBody](../../models/components/createprojectrequestbody.md) | :heavy_check_mark:                                                                         | N/A                                                                                        |