# SkillContentFile

A supporting file of a skill directory, uploaded with its SKILL.md.


## Fields

| Field                                                                             | Type                                                                              | Required                                                                          | Description                                                                       |
| --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- | --------------------------------------------------------------------------------- |
| `Content`                                                                         | `[]byte`                                                                          | :heavy_check_mark:                                                                | Exact file bytes.                                                                 |
| `Path`                                                                            | `string`                                                                          | :heavy_check_mark:                                                                | Slash-separated path relative to the skill directory, for example scripts/run.sh. |
| `Sha256`                                                                          | `string`                                                                          | :heavy_check_mark:                                                                | Lowercase SHA-256 of content.                                                     |