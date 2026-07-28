# GetToolsetRequest

## Example Usage

```typescript
import { GetToolsetRequest } from "@gram/client/models/operations/gettoolset.js";

let value: GetToolsetRequest = {
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
