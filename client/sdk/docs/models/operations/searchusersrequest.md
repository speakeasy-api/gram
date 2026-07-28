# SearchUsersRequest

## Example Usage

```typescript
import { SearchUsersRequest } from "@gram/client/models/operations/searchusers.js";

let value: SearchUsersRequest = {
  searchUsersPayload: {
    filter: {
      from: new Date("2025-12-19T10:00:00Z"),
      to: new Date("2025-12-19T11:00:00Z"),
    },
    userType: "external",
  },
};
```

## Fields

| Field                | Type                                                                           | Required           | Description    |
| -------------------- | ------------------------------------------------------------------------------ | ------------------ | -------------- |
| `gramKey`            | _string_                                                                       | :heavy_minus_sign: | API Key header |
| `gramSession`        | _string_                                                                       | :heavy_minus_sign: | Session header |
| `gramProject`        | _string_                                                                       | :heavy_minus_sign: | project header |
| `searchUsersPayload` | [components.SearchUsersPayload](../../models/components/searchuserspayload.md) | :heavy_check_mark: | N/A            |
