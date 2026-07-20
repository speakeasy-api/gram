# UpdateDestinationRequestBody

## Example Usage

```typescript
import { UpdateDestinationRequestBody } from "@gram/client/models/components";

let value: UpdateDestinationRequestBody = {
  enabled: true,
  endpointUrl: "https://uneven-cook.org",
  id: "<id>",
  name: "<value>",
};
```

## Fields

| Field         | Type                                                                                           | Required           | Description                                                   |
| ------------- | ---------------------------------------------------------------------------------------------- | ------------------ | ------------------------------------------------------------- |
| `enabled`     | _boolean_                                                                                      | :heavy_check_mark: | Updated enabled state.                                        |
| `endpointUrl` | _string_                                                                                       | :heavy_check_mark: | Updated URL.                                                  |
| `headers`     | [components.OtelForwardingHeaderInput](../../models/components/otelforwardingheaderinput.md)[] | :heavy_minus_sign: | Full set of headers to attach. Replaces any existing headers. |
| `id`          | _string_                                                                                       | :heavy_check_mark: | The destination ID.                                           |
| `name`        | _string_                                                                                       | :heavy_check_mark: | Updated name.                                                 |
