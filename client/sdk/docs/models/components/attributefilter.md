# AttributeFilter

Filter on a log attribute by path.

## Example Usage

```typescript
import { AttributeFilter } from "@gram/client/models/components";

let value: AttributeFilter = {
  path: "@user.region",
  value: "us-east-1",
};
```

## Fields

| Field   | Type                                           | Required           | Description                                                                                                                       | Example      |
| ------- | ---------------------------------------------- | ------------------ | --------------------------------------------------------------------------------------------------------------------------------- | ------------ |
| `op`    | [components.Op](../../models/components/op.md) | :heavy_minus_sign: | Comparison operator                                                                                                               |              |
| `path`  | _string_                                       | :heavy_check_mark: | Attribute path. Use @ prefix for custom attributes (e.g. '@user.region'), or bare path for system attributes (e.g. 'http.route'). | @user.region |
| `value` | _string_                                       | :heavy_minus_sign: | Value to compare against (ignored for 'exists' and 'not_exists' operators)                                                        | us-east-1    |
