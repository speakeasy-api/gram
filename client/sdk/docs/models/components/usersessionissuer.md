# UserSessionIssuer

A user_session_issuer record.

## Example Usage

```typescript
import { UserSessionIssuer } from "@gram/client/models/components/usersessionissuer.js";

let value: UserSessionIssuer = {
  authnChallengeMode: "<value>",
  createdAt: new Date("2026-07-17T09:59:24.504Z"),
  id: "caefc357-34f0-4d02-a1c3-0ff39853433a",
  projectId: "5637521c-ec0f-4a6b-b34d-1b76082a8b86",
  sessionDurationHours: 266923,
  slug: "<value>",
  updatedAt: new Date("2024-02-22T05:46:28.891Z"),
};
```

## Fields

| Field                  | Type                                                                                          | Required           | Description                             |
| ---------------------- | --------------------------------------------------------------------------------------------- | ------------------ | --------------------------------------- |
| `authnChallengeMode`   | _string_                                                                                      | :heavy_check_mark: | chain \| interactive.                   |
| `createdAt`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                     |
| `id`                   | _string_                                                                                      | :heavy_check_mark: | The user_session_issuer id.             |
| `projectId`            | _string_                                                                                      | :heavy_check_mark: | The owning project id.                  |
| `sessionDurationHours` | _number_                                                                                      | :heavy_check_mark: | Issued user session lifetime, in hours. |
| `slug`                 | _string_                                                                                      | :heavy_check_mark: | Project-unique slug.                    |
| `updatedAt`            | [Date](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Reference/Global_Objects/Date) | :heavy_check_mark: | N/A                                     |
