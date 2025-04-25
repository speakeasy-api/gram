# Environment

Model representing an environment


## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `created_at`                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | The creation date of the environment                                 |
| `description`                                                        | *Optional[str]*                                                      | :heavy_minus_sign:                                                   | The description of the environment                                   |
| `entries`                                                            | List[[models.EnvironmentEntry](../models/environmententry.md)]       | :heavy_check_mark:                                                   | List of environment entries                                          |
| `id`                                                                 | *str*                                                                | :heavy_check_mark:                                                   | The ID of the environment                                            |
| `name`                                                               | *str*                                                                | :heavy_check_mark:                                                   | The name of the environment                                          |
| `organization_id`                                                    | *str*                                                                | :heavy_check_mark:                                                   | The organization ID this environment belongs to                      |
| `project_id`                                                         | *str*                                                                | :heavy_check_mark:                                                   | The project ID this environment belongs to                           |
| `slug`                                                               | *str*                                                                | :heavy_check_mark:                                                   | N/A                                                                  |
| `updated_at`                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | When the environment was last updated                                |