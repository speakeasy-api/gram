# ListProjectsRequest

## Example Usage

```typescript
import { ListProjectsRequest } from "@gram/client/models/operations/listprojects.js";

let value: ListProjectsRequest = {
  organizationId: "<id>",
};
```

## Fields

| Field            | Type     | Required           | Description                                     |
| ---------------- | -------- | ------------------ | ----------------------------------------------- |
| `organizationId` | _string_ | :heavy_check_mark: | The ID of the organization to list projects for |
| `gramKey`        | _string_ | :heavy_minus_sign: | API Key header                                  |
| `gramSession`    | _string_ | :heavy_minus_sign: | Session header                                  |
