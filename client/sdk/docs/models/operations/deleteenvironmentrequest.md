# DeleteEnvironmentRequest

## Example Usage

```typescript
import { DeleteEnvironmentRequest } from "@gram/client/models/operations/deleteenvironment.js";

let value: DeleteEnvironmentRequest = {
  slug: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                           |
| ------------- | -------- | ------------------ | ------------------------------------- |
| `slug`        | _string_ | :heavy_check_mark: | The slug of the environment to delete |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                        |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                        |
