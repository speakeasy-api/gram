# OtelForwardingHeaderInput

HTTP header value provided when upserting a forwarding config.

## Example Usage

```typescript
import { OtelForwardingHeaderInput } from "@gram/client/models/components/otelforwardingheaderinput.js";

let value: OtelForwardingHeaderInput = {
  name: "<value>",
  value: "<value>",
};
```

## Fields

| Field   | Type     | Required           | Description                                                      |
| ------- | -------- | ------------------ | ---------------------------------------------------------------- |
| `name`  | _string_ | :heavy_check_mark: | Header name.                                                     |
| `value` | _string_ | :heavy_check_mark: | Header value. Stored encrypted at rest; never returned on reads. |
