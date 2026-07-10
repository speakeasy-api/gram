# ListShadowMCPApprovalsRequest

## Example Usage

```typescript
import { ListShadowMCPApprovalsRequest } from "@gram/client/models/operations";

let value: ListShadowMCPApprovalsRequest = {
  policyId: "f1c49183-8487-42fb-87aa-b33c7ca83b4d",
};
```

## Fields

| Field               | Type                | Required            | Description         |
| ------------------- | ------------------- | ------------------- | ------------------- |
| `policyId`          | *string*            | :heavy_check_mark:  | The risk policy ID. |
| `gramKey`           | *string*            | :heavy_minus_sign:  | API Key header      |
| `gramSession`       | *string*            | :heavy_minus_sign:  | Session header      |
| `gramProject`       | *string*            | :heavy_minus_sign:  | project header      |