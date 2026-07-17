---
"server": patch
---

Persist company-credential AI accounts in `user_accounts`. Claude sessions authenticated by an API key, bearer-token gateway, Bedrock, or Vertex carry no `user.account_uuid`, so attribution previously classified them (`account_type=team`) but never persisted an account entity — leaving no chat→account links and no billing-mode resolution for the whole population. Such sessions now upsert an entity keyed on the resolved employee — so all of an employee's sessions (different reported emails, device-bridge attribution, or no email at all) converge on one account — falling back to the normalized session email while no employee has resolved; an email-keyed row is promoted to the employee key in place when its email later resolves. Company-credential accounts run the billing-mode cascade like OAuth accounts, and a chat linked to one upgrades to the OAuth account if the session's `user.account_uuid` arrives in a later batch.
