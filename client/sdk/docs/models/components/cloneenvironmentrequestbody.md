# CloneEnvironmentRequestBody

## Example Usage

```typescript
import { CloneEnvironmentRequestBody } from "@gram/client/models/components/cloneenvironmentrequestbody.js";

let value: CloneEnvironmentRequestBody = {
  newName: "<value>",
};
```

## Fields

| Field        | Type      | Required           | Description                                                                                                                            |
| ------------ | --------- | ------------------ | -------------------------------------------------------------------------------------------------------------------------------------- |
| `copyValues` | _boolean_ | :heavy_minus_sign: | If true, copy the encrypted secret values from the source. If false (default), copy only variable names with empty placeholder values. |
| `newName`    | _string_  | :heavy_check_mark: | The name for the new cloned environment                                                                                                |
