# CreateEnvironmentRequest

## Example Usage

```typescript
import { CreateEnvironmentRequest } from "@gram/client/models/operations";

let value: CreateEnvironmentRequest = {
  createEnvironmentForm: {
    entries: [
      {
        name: "<value>",
        value: "<value>",
      },
    ],
    name: "<value>",
    organizationId: "<id>",
  },
};
```

## Fields

| Field                                                                                | Type                                                                                 | Required                                                                             | Description                                                                          |
| ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------ |
| `gramSession`                                                                        | *string*                                                                             | :heavy_minus_sign:                                                                   | Session header                                                                       |
| `gramProject`                                                                        | *string*                                                                             | :heavy_minus_sign:                                                                   | project header                                                                       |
| `createEnvironmentForm`                                                              | [components.CreateEnvironmentForm](../../models/components/createenvironmentform.md) | :heavy_check_mark:                                                                   | N/A                                                                                  |