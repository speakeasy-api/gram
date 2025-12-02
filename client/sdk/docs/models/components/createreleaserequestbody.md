# CreateReleaseRequestBody

## Example Usage

```typescript
import { CreateReleaseRequestBody } from "@gram/client/models/components";

let value: CreateReleaseRequestBody = {
  toolsetSlug: "<value>",
};
```

## Fields

| Field                                           | Type                                            | Required                                        | Description                                     |
| ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- | ----------------------------------------------- |
| `notes`                                         | *string*                                        | :heavy_minus_sign:                              | Optional release notes                          |
| `toolsetSlug`                                   | *string*                                        | :heavy_check_mark:                              | The slug of the toolset to create a release for |