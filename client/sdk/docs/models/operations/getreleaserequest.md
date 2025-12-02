# GetReleaseRequest

## Example Usage

```typescript
import { GetReleaseRequest } from "@gram/client/models/operations";

let value: GetReleaseRequest = {
  releaseId: "<id>",
};
```

## Fields

| Field                 | Type                  | Required              | Description           |
| --------------------- | --------------------- | --------------------- | --------------------- |
| `releaseId`           | *string*              | :heavy_check_mark:    | The ID of the release |
| `gramSession`         | *string*              | :heavy_minus_sign:    | Session header        |
| `gramKey`             | *string*              | :heavy_minus_sign:    | API Key header        |
| `gramProject`         | *string*              | :heavy_minus_sign:    | project header        |