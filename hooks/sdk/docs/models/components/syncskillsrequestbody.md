# SyncSkillsRequestBody

A complete snapshot of Gram-managed skills and current application exceptions for one machine.


## Fields

| Field                                                                            | Type                                                                             | Required                                                                         | Description                                                                      |
| -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- | -------------------------------------------------------------------------------- |
| `Exceptions`                                                                     | [][components.SyncSkillException](../../models/components/syncskillexception.md) | :heavy_check_mark:                                                               | All current failures to apply distributed skills on the machine.                 |
| `Installed`                                                                      | [][components.SyncSkillInstalled](../../models/components/syncskillinstalled.md) | :heavy_check_mark:                                                               | All Gram-managed skills currently installed on the machine.                      |
| `Provider`                                                                       | [components.Provider](../../models/components/provider.md)                       | :heavy_check_mark:                                                               | The local coding assistant provider.                                             |