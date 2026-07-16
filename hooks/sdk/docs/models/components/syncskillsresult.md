# SyncSkillsResult

The deterministic delta from the submitted complete snapshot to the user's current visible distributions.


## Fields

| Field                                                                      | Type                                                                       | Required                                                                   | Description                                                                |
| -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| `Removals`                                                                 | []`string`                                                                 | :heavy_check_mark:                                                         | Installed Gram-managed skill names no longer visible to this user.         |
| `Updates`                                                                  | [][components.SyncSkillUpdate](../../models/components/syncskillupdate.md) | :heavy_check_mark:                                                         | New or changed skill manifests to write.                                   |