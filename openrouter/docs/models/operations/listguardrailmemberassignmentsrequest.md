# ListGuardrailMemberAssignmentsRequest


## Fields

| Field                                         | Type                                          | Required                                      | Description                                   | Example                                       |
| --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- | --------------------------------------------- |
| `ID`                                          | *string*                                      | :heavy_check_mark:                            | The unique identifier of the guardrail        | 550e8400-e29b-41d4-a716-446655440000          |
| `Offset`                                      | **string*                                     | :heavy_minus_sign:                            | Number of records to skip for pagination      | 0                                             |
| `Limit`                                       | **string*                                     | :heavy_minus_sign:                            | Maximum number of records to return (max 100) | 50                                            |