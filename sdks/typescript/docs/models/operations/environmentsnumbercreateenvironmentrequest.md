# EnvironmentsNumberCreateEnvironmentRequest

## Example Usage

```typescript
import { EnvironmentsNumberCreateEnvironmentRequest } from "@gram/sdk/models/operations";

let value: EnvironmentsNumberCreateEnvironmentRequest = {
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