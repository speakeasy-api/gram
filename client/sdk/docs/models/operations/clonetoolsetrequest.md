# CloneToolsetRequest

## Example Usage

```typescript
import { CloneToolsetRequest } from "@gram/client/models/operations/clonetoolset.js";

let value: CloneToolsetRequest = {
  slug: "<value>",
};
```

## Fields

| Field         | Type     | Required           | Description                      |
| ------------- | -------- | ------------------ | -------------------------------- |
| `slug`        | _string_ | :heavy_check_mark: | The slug of the toolset to clone |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                   |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                   |
