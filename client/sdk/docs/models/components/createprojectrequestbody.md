# CreateProjectRequestBody

## Example Usage

```typescript
import { CreateProjectRequestBody } from "@gram/client/models/components/createprojectrequestbody.js";

let value: CreateProjectRequestBody = {
  name: "<value>",
  organizationId: "<id>",
};
```

## Fields

| Field            | Type     | Required           | Description                                         |
| ---------------- | -------- | ------------------ | --------------------------------------------------- |
| `name`           | _string_ | :heavy_check_mark: | The name of the project                             |
| `organizationId` | _string_ | :heavy_check_mark: | The ID of the organization to create the project in |
