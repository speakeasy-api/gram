# SyncSkillUpdate

A skill manifest the caller should write to local managed state.


## Fields

| Field                                                        | Type                                                         | Required                                                     | Description                                                  |
| ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ | ------------------------------------------------------------ |
| `Content`                                                    | `string`                                                     | :heavy_check_mark:                                           | The complete SKILL.md content to write.                      |
| `Description`                                                | `*string`                                                    | :heavy_minus_sign:                                           | The optional description from the resolved manifest version. |
| `Name`                                                       | `string`                                                     | :heavy_check_mark:                                           | The normalized skill name.                                   |
| `RawSha256`                                                  | `string`                                                     | :heavy_check_mark:                                           | The SHA-256 digest of content.                               |