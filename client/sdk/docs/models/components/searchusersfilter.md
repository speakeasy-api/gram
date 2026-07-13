# SearchUsersFilter

Filter criteria for searching user usage summaries

## Example Usage

```typescript
import { SearchUsersFilter } from "@gram/client/models/components/searchusersfilter.js";

let value: SearchUsersFilter = {
  from: new Date("2025-12-19T10:00:00Z"),
  to: new Date("2025-12-19T11:00:00Z"),
};
```

## Fields

| Field           | Type                                                                                          | Required           | Description                                                                                                                        | Example              |
| --------------- | --------------------------------------------------------------------------------------------- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------- | -------------------- |
| `accountType`   | _string_                                                                                      | :heavy_minus_sign: | Optional account type filter ('team' or 'personal').                                                                               |                      |
| `deploymentId`  | _string_                                                                                      | :heavy_minus_sign: | Deployment ID filter                                                                                                               |                      |
| `eventSource`   | _string_                                                                                      | :heavy_minus_sign: | Optional event source filter (e.g. 'hook'). When set, only rows with a matching event_source are included.                         |                      |
| `externalOrgId` | _string_                                                                                      | :heavy_minus_sign: | Optional filter to a single AI account by its provider org id (the per-account discriminator); scopes results to that one account. |                      |
| `from`          | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | Start time in ISO 8601 format (e.g., '2025-12-19T10:00:00Z')                                                                       | 2025-12-19T10:00:00Z |
| `hookSource`    | _string_                                                                                      | :heavy_minus_sign: | Optional hook source filter (e.g. 'cursor', 'claude-code').                                                                        |                      |
| `to`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | End time in ISO 8601 format (e.g., '2025-12-19T11:00:00Z')                                                                         | 2025-12-19T11:00:00Z |
| `userIds`       | _string_[]                                                                                    | :heavy_minus_sign: | Optional list of user identifiers to include. Matches user_id for internal searches and external_user_id for external searches.    |                      |
