# PublishRequest

## Example Usage

```typescript
import { PublishRequest } from "@gram/client/models/operations/publish.js";

let value: PublishRequest = {
  publishPackageForm: {
    deploymentId: "<id>",
    name: "<value>",
    version: "<value>",
    visibility: "public",
  },
};
```

## Fields

| Field                | Type                                                                           | Required           | Description    |
| -------------------- | ------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`            | _string_                                                                       | :heavy_minus_sign: | API Key header |
| `gramSession`        | _string_                                                                       | :heavy_minus_sign: | Session header |
| `gramProject`        | _string_                                                                       | :heavy_minus_sign: | project header |
| `publishPackageForm` | [components.PublishPackageForm](../../models/components/publishpackageform.md) | :heavy_check_mark: | N/A            |
