# DeleteSourceEnvironmentLinkRequest

## Example Usage

```typescript
import { DeleteSourceEnvironmentLinkRequest } from "@gram/client/models/operations";

let value: DeleteSourceEnvironmentLinkRequest = {
  sourceKind: "function",
  sourceSlug: "<value>",
};
```

## Fields

| Field                                                          | Type                                                           | Required                                                       | Description                                                    |
| -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- | -------------------------------------------------------------- |
| `sourceKind`                                                   | [operations.SourceKind](../../models/operations/sourcekind.md) | :heavy_check_mark:                                             | The kind of source (http or function)                          |
| `sourceSlug`                                                   | *string*                                                       | :heavy_check_mark:                                             | The slug of the source                                         |
| `gramSession`                                                  | *string*                                                       | :heavy_minus_sign:                                             | Session header                                                 |
| `gramProject`                                                  | *string*                                                       | :heavy_minus_sign:                                             | project header                                                 |