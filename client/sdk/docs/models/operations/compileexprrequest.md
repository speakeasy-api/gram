# CompileExprRequest

## Example Usage

```typescript
import { CompileExprRequest } from "@gram/client/models/operations/compileexpr.js";

let value: CompileExprRequest = {};
```

## Fields

| Field         | Type     | Required           | Description                                                            |
| ------------- | -------- | ------------------ | ---------------------------------------------------------------------- |
| `expr`        | _string_ | :heavy_minus_sign: | The CEL expression to compile. Empty is valid and compiles to ok=true. |
| `gramKey`     | _string_ | :heavy_minus_sign: | API Key header                                                         |
| `gramSession` | _string_ | :heavy_minus_sign: | Session header                                                         |
| `gramProject` | _string_ | :heavy_minus_sign: | project header                                                         |
