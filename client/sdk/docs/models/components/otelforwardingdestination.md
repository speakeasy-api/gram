# OtelForwardingDestination

A configured OTEL forwarding endpoint owned by an organization (optionally scoped to a project).

## Example Usage

```typescript
import { OtelForwardingDestination } from "@gram/client/models/components";

let value: OtelForwardingDestination = {
  createdAt: new Date("2026-11-07T02:04:54.657Z"),
  enabled: false,
  endpointUrl: "https://polished-teriyaki.net/",
  headers: [
    {
      hasValue: false,
      name: "<value>",
    },
  ],
  id: "<id>",
  name: "<value>",
  organizationId: "<id>",
  updatedAt: new Date("2026-04-08T06:51:01.755Z"),
};
```

## Fields

| Field            | Type                                                                                          | Required           | Description                                                            |
| ---------------- | --------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------- |
| `createdAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | ISO 8601 timestamp when the destination was created.                   |
| `enabled`        | _boolean_                                                                                     | :heavy_check_mark: | Whether forwarding to this destination is currently active.            |
| `endpointUrl`    | _string_                                                                                      | :heavy_check_mark: | URL each OTEL payload is POSTed to.                                    |
| `headers`        | [components.OtelForwardingHeader](../../models/components/otelforwardingheader.md)[]          | :heavy_check_mark: | Headers configured for this destination. Values are never returned.    |
| `id`             | _string_                                                                                      | :heavy_check_mark: | Destination ID.                                                        |
| `name`           | _string_                                                                                      | :heavy_check_mark: | Human-readable name. Unique within (org, project).                     |
| `organizationId` | _string_                                                                                      | :heavy_check_mark: | Organization the destination belongs to.                               |
| `projectId`      | _string_                                                                                      | :heavy_minus_sign: | Project the destination belongs to. Omitted for org-wide destinations. |
| `updatedAt`      | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | ISO 8601 timestamp of the most recent change.                          |
