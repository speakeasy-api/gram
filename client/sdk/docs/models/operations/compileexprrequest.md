# CompileExprRequest

## Example Usage

```typescript
import { CompileExprRequest } from "@gram/client/models/operations/compileexpr.js";

let value: CompileExprRequest = {};
```

## Fields

| Field                                                                  | Type                                                                   | Required                                                               | Description                                                            |
| ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------- |
| `expr`                                                                 | *string*                                                               | :heavy_minus_sign:                                                     | The CEL expression to compile. Empty is valid and compiles to ok=true. |
| `gramKey`                                                              | *string*                                                               | :heavy_minus_sign:                                                     | API Key header                                                         |
| `gramSession`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | Session header                                                         |
| `gramProject`                                                          | *string*                                                               | :heavy_minus_sign:                                                     | project header                                                         |