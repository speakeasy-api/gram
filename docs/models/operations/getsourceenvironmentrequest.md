# GetSourceEnvironmentRequest

## Example Usage

```typescript
import { GetSourceEnvironmentRequest } from "@gram/client/models/operations";

let value: GetSourceEnvironmentRequest = {
  sourceKind: "function",
  sourceSlug: "<value>",
};
```

## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `sourceKind`                                                                       | [operations.QueryParamSourceKind](../../models/operations/queryparamsourcekind.md) | :heavy_check_mark:                                                                 | The kind of source (http or function)                                              |
| `sourceSlug`                                                                       | *string*                                                                           | :heavy_check_mark:                                                                 | The slug of the source                                                             |
| `gramSession`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | Session header                                                                     |
| `gramProject`                                                                      | *string*                                                                           | :heavy_minus_sign:                                                                 | project header                                                                     |