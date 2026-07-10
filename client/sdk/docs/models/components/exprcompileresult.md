# ExprCompileResult

The result of compiling a single CEL expression for the editor.

## Example Usage

```typescript
import { ExprCompileResult } from "@gram/client/models/components/exprcompileresult.js";

let value: ExprCompileResult = {
  error: "<value>",
  ok: true,
};
```

## Fields

| Field                                                     | Type                                                      | Required                                                  | Description                                               |
| --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- | --------------------------------------------------------- |
| `error`                                                   | *string*                                                  | :heavy_check_mark:                                        | Compiler error message when ok is false; empty otherwise. |
| `ok`                                                      | *boolean*                                                 | :heavy_check_mark:                                        | True when the expression compiled successfully.           |