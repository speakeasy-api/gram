# RollbackToReleaseRequestBody

## Example Usage

```typescript
import { RollbackToReleaseRequestBody } from "@gram/client/models/components";

let value: RollbackToReleaseRequestBody = {
  releaseNumber: 400951,
  toolsetSlug: "<value>",
};
```

## Fields

| Field                             | Type                              | Required                          | Description                       |
| --------------------------------- | --------------------------------- | --------------------------------- | --------------------------------- |
| `releaseNumber`                   | *number*                          | :heavy_check_mark:                | The release number to rollback to |
| `toolsetSlug`                     | *string*                          | :heavy_check_mark:                | The slug of the toolset           |