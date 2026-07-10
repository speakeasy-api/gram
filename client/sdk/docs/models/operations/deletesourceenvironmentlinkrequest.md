# DeleteSourceEnvironmentLinkRequest

## Example Usage

```typescript
import { DeleteSourceEnvironmentLinkRequest } from "@gram/client/models/operations/deletesourceenvironmentlink.js";

let value: DeleteSourceEnvironmentLinkRequest = {
  sourceKind: "function",
  sourceSlug: "<value>",
};
```

## Fields

| Field         | Type                                                           | Required           | Description                           |
| ------------- | -------------------------------------------------------------- | ------------------ | ------------------------------------- |
| `sourceKind`  | [operations.SourceKind](../../models/operations/sourcekind.md) | :heavy_check_mark: | The kind of source (http or function) |
| `sourceSlug`  | _string_                                                       | :heavy_check_mark: | The slug of the source                |
| `gramSession` | _string_                                                       | :heavy_minus_sign: | Session header                        |
| `gramProject` | _string_                                                       | :heavy_minus_sign: | project header                        |
