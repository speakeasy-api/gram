# RemoveGrantsRequest

## Example Usage

```typescript
import { RemoveGrantsRequest } from "@gram/client/models/operations";

let value: RemoveGrantsRequest = {
  grantsForm: {
    grants: [
      {
        principalUrn: "<value>",
        resource: "<value>",
        scope: "<value>",
      },
    ],
  },
};
```

## Fields

| Field         | Type                                                           | Required           | Description    |
| ------------- | -------------------------------------------------------------- | ------------------ | -------------- |
| `gramKey`     | _string_                                                       | :heavy_minus_sign: | API Key header |
| `gramSession` | _string_                                                       | :heavy_minus_sign: | Session header |
| `grantsForm`  | [components.GrantsForm](../../models/components/grantsform.md) | :heavy_check_mark: | N/A            |
