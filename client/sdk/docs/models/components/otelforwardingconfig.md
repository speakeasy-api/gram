# OtelForwardingConfig

Per-organization config that controls forwarding of OTEL payloads received on the hooks endpoints to a customer-owned URL. When no config is set, id/created_at/updated_at are omitted and enabled defaults to false.

## Example Usage

```typescript
import { OtelForwardingConfig } from "@gram/client/models/components/otelforwardingconfig.js";

let value: OtelForwardingConfig = {
  enabled: false,
  endpointUrl: "https://pleasant-pharmacopoeia.info",
  headers: [
    {
      hasValue: false,
      name: "<value>",
    },
  ],
  organizationId: "<id>",
};
```

## Fields

| Field                                                                                         | Type                                                                                          | Required                                                                                      | Description                                                                                   |
| --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- | --------------------------------------------------------------------------------------------- |
| `createdAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | ISO 8601 timestamp when the config was created. Omitted when no config is set.                |
| `enabled`                                                                                     | *boolean*                                                                                     | :heavy_check_mark:                                                                            | Whether forwarding is currently active.                                                       |
| `endpointUrl`                                                                                 | *string*                                                                                      | :heavy_check_mark:                                                                            | URL each OTEL payload is POSTed to. Empty string when no config is set.                       |
| `headers`                                                                                     | [components.OtelForwardingHeader](../../models/components/otelforwardingheader.md)[]          | :heavy_check_mark:                                                                            | Headers configured for this endpoint. Values are never returned.                              |
| `id`                                                                                          | *string*                                                                                      | :heavy_minus_sign:                                                                            | Config ID. Omitted when no config is set for the organization.                                |
| `organizationId`                                                                              | *string*                                                                                      | :heavy_check_mark:                                                                            | Organization the config belongs to.                                                           |
| `updatedAt`                                                                                   | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_minus_sign:                                                                            | ISO 8601 timestamp of the most recent change. Omitted when no config is set.                  |