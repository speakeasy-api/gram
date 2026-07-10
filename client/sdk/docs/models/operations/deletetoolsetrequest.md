# DeleteToolsetRequest

## Example Usage

```typescript
import { DeleteToolsetRequest } from "@gram/client/models/operations/deletetoolset.js";

let value: DeleteToolsetRequest = {
  slug: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description             |
| ------------- | -------- | ------------------ | ----------------------- |
| `slug`        | _string_ | :heavy_check_mark: | The slug of the toolset |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header          |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header          |
| `gramProject` | _string_ | :heavy_minus_sign: | project header          |
