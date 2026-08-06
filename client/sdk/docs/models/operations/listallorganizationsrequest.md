# ListAllOrganizationsRequest

## Example Usage

```typescript
import { ListAllOrganizationsRequest } from "@gram/client/models/operations";

let value: ListAllOrganizationsRequest = {};
```

## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `limit`                                                 | *number*                                                | :heavy_minus_sign:                                      | Maximum organizations to return (default 100, max 500). |
| `offset`                                                | *number*                                                | :heavy_minus_sign:                                      | Number of organizations to skip.                        |
| `gramKey`                                               | *string*                                                | :heavy_minus_sign:                                      | API Key header                                          |