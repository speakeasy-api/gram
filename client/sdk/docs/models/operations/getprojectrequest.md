# GetProjectRequest

## Example Usage

```typescript
import { GetProjectRequest } from "@gram/client/models/operations/getproject.js";

let value: GetProjectRequest = {
  slug: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                    |
| ------------- | -------- | ------------------ | ------------------------------ |
| `slug`        | _string_ | :heavy_check_mark: | The slug of the project to get |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                 |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                 |
