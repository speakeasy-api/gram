# ListProjectsRequest

## Example Usage

```typescript
import { ListProjectsRequest } from "@gram/client/models/operations";

let value: ListProjectsRequest = {
  organizationId: "<id>",
};
```

## Fields

| Field                                           | Type                                            | Required                                        | Description                                     |
| ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `organizationId`                                | *string*                                        | :heavy_check_mark:                              | The ID of the organization to list projects for |
| `gramKey`                                       | *string*                                        | :heavy_minus_sign:                              | API Key header                                  |
| `gramSession`                                   | *string*                                        | :heavy_minus_sign:                              | Session header                                  |