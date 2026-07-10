# UserAccount

A linked AI account for a user. The identity is (provider, email): the same email registered on two providers is two distinct accounts.

## Example Usage

```typescript
import { UserAccount } from "@gram/client/models/components/useraccount.js";

let value: UserAccount = {
  provider: "<value>",
};
```

## Fields

| Field                                                                                                       | Type                                                                                                        | Required                                                                                                    | Description                                                                                                 |
| ----------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `accountType`                                                                                               | *string*                                                                                                    | :heavy_minus_sign:                                                                                          | 'team' (enterprise) or 'personal' (individual); empty when not yet classified                               |
| `email`                                                                                                     | *string*                                                                                                    | :heavy_minus_sign:                                                                                          | Email associated with the account; may differ from the user's work email for personal accounts              |
| `externalOrgId`                                                                                             | *string*                                                                                                    | :heavy_minus_sign:                                                                                          | Provider org id for this account; the per-account discriminator used to scope telemetry to this one account |
| `id`                                                                                                        | *string*                                                                                                    | :heavy_minus_sign:                                                                                          | Account record id (user_accounts.id); used to scope chat/session views to this account                      |
| `lastSeenUnixNano`                                                                                          | *string*                                                                                                    | :heavy_minus_sign:                                                                                          | Latest activity timestamp for this account in Unix nanoseconds                                              |
| `provider`                                                                                                  | *string*                                                                                                    | :heavy_check_mark:                                                                                          | AI provider the account belongs to ('anthropic', 'openai', 'cursor')                                        |