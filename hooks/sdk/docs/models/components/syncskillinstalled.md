# SyncSkillInstalled

A Gram-managed skill currently present on the caller's machine.


## Fields

| Field                                                   | Type                                                    | Required                                                | Description                                             |
| ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- | ------------------------------------------------------- |
| `Name`                                                  | `string`                                                | :heavy_check_mark:                                      | The normalized skill name.                              |
| `RawSha256`                                             | `string`                                                | :heavy_check_mark:                                      | The SHA-256 digest of the exact local SKILL.md content. |