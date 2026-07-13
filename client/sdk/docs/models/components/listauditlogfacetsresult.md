# ListAuditLogFacetsResult

## Example Usage

```typescript
import { ListAuditLogFacetsResult } from "@gram/client/models/components/listauditlogfacetsresult.js";

let value: ListAuditLogFacetsResult = {
  actions: [
    {
      count: 267185,
      displayName: "Junius73",
      value: "<value>",
    },
  ],
  actors: [],
};
```

## Fields

| Field     | Type                                                                               | Required           | Description             |
| --------- | ---------------------------------------------------------------------------------- | ------------------ | ----------------------- |
| `actions` | [components.AuditLogFacetOption](../../models/components/auditlogfacetoption.md)[] | :heavy_check_mark: | Available action facets |
| `actors`  | [components.AuditLogFacetOption](../../models/components/auditlogfacetoption.md)[] | :heavy_check_mark: | Available actor facets  |
