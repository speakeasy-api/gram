# EnvironmentEntry

A single environment entry


## Fields

| Field                                                                | Type                                                                 | Required                                                             | Description                                                          |
| -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- | -------------------------------------------------------------------- |
| `created_at`                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | The creation date of the environment entry                           |
| `name`                                                               | *str*                                                                | :heavy_check_mark:                                                   | The name of the environment variable                                 |
| `updated_at`                                                         | [date](https://docs.python.org/3/library/datetime.html#date-objects) | :heavy_check_mark:                                                   | When the environment entry was last updated                          |
| `value`                                                              | *str*                                                                | :heavy_check_mark:                                                   | Redacted values of the environment variable                          |