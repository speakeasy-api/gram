# HookSkillData

Skill activation payload.


## Fields

| Field                                                  | Type                                                   | Required                                               | Description                                            |
| ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ | ------------------------------------------------------ |
| `Name`                                                 | `string`                                               | :heavy_check_mark:                                     | Activated skill name.                                  |
| `RawSha256`                                            | `*string`                                              | :heavy_minus_sign:                                     | SHA-256 of the raw skill manifest, if available.       |
| `Source`                                               | `*string`                                              | :heavy_minus_sign:                                     | Skill source or namespace, if available.               |
| `SourceLevel`                                          | `*string`                                              | :heavy_minus_sign:                                     | Scope where the skill was resolved, if available.      |
| `SourcePath`                                           | `*string`                                              | :heavy_minus_sign:                                     | Local path where the skill was resolved, if available. |