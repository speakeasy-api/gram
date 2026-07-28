# ListVersionsRequest

## Example Usage

```typescript
import { ListVersionsRequest } from "@gram/client/models/operations/listversions.js";

let value: ListVersionsRequest = {
  name: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description             |
| ------------- | -------- | ------------------ | ----------------------- |
| `name`        | _string_ | :heavy_check_mark: | The name of the package |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header          |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header          |
| `gramProject` | _string_ | :heavy_minus_sign: | project header          |
