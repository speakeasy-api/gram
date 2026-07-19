# UploadSkillContentPayload

Content for a skill manifest requested by a prior hook ingest response.


## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `Content`                                                            | `string`                                                             | :heavy_check_mark:                                                   | Raw UTF-8 skill manifest content.                                    |
| `RawSha256`                                                          | `string`                                                             | :heavy_check_mark:                                                   | Lowercase SHA-256 of the raw content.                                |
| `SchemaVersion`                                                      | [components.SchemaVersion](../../models/components/schemaversion.md) | :heavy_check_mark:                                                   | Contract version.                                                    |