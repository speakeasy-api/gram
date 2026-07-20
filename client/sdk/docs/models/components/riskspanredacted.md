# RiskSpanRedacted

## Example Usage

```typescript
import { RiskSpanRedacted } from "@gram/client/models/components/riskspanredacted.js";

let value: RiskSpanRedacted = {
  matchRedacted: "<value>",
  positionKnown: false,
};
```

## Fields

| Field           | Type      | Required           | Description                                                                                     |
| --------------- | --------- | ------------------ | ----------------------------------------------------------------------------------------------- |
| `field`         | _string_  | :heavy_minus_sign: | The message field this span matched (see RiskSpan.field).                                       |
| `matchRedacted` | _string_  | :heavy_check_mark: | Opaque fingerprint of this span's match, in the same form as RiskResultRedacted.match_redacted. |
| `path`          | _string_  | :heavy_minus_sign: | The JSON sub-path within the field for a `.get(...)` match (see RiskSpan.path).                 |
| `positionKnown` | _boolean_ | :heavy_check_mark: | Whether this span carried byte-position information.                                            |
