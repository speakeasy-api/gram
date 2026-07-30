# UpsertGrantsResult

## Example Usage

```typescript
import { UpsertGrantsResult } from "@gram/client/models/components";

let value: UpsertGrantsResult = {
  grants: [],
};
```

## Fields

| Field    | Type                                                   | Required           | Description                                           |
| -------- | ------------------------------------------------------ | ------------------ | ----------------------------------------------------- |
| `grants` | [components.Grant](../../models/components/grant.md)[] | :heavy_check_mark: | The permissions that were created or already existed. |
