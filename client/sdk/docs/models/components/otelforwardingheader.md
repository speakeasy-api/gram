# OtelForwardingHeader

HTTP header forwarded with each OTEL payload.

## Example Usage

```typescript
import { OtelForwardingHeader } from "@gram/client/models/components/otelforwardingheader.js";

let value: OtelForwardingHeader = {
  hasValue: true,
  name: "<value>",
};
```

## Fields

| Field                                                                                                 | Type                                                                                                  | Required                                                                                              | Description                                                                                           |
| ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| `hasValue`                                                                                            | *boolean*                                                                                             | :heavy_check_mark:                                                                                    | Whether a non-empty value is currently stored for this header. Always false on write-only operations. |
| `name`                                                                                                | *string*                                                                                              | :heavy_check_mark:                                                                                    | Header name.                                                                                          |