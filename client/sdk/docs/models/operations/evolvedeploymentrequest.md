# EvolveDeploymentRequest

## Example Usage

```typescript
import { EvolveDeploymentRequest } from "@gram/client/models/operations";

let value: EvolveDeploymentRequest = {
  evolveForm: {},
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `gramSession`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Session header                                                 |
| `gramProject`                                                  | *string*                                                       | :heavy_minus_sign:                                             | project header                                                 |
| `evolveForm`                                                   | [components.EvolveForm](../../models/components/evolveform.md) | :heavy_check_mark:                                             | N/A                                                            |