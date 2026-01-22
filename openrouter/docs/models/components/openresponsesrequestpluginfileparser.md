# OpenResponsesRequestPluginFileParser


## Fields

| Field                                                                              | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `ID`                                                                               | [components.IDFileParser](../../models/components/idfileparser.md)                 | :heavy_check_mark:                                                                 | N/A                                                                                |
| `Enabled`                                                                          | **bool*                                                                            | :heavy_minus_sign:                                                                 | Set to false to disable the file-parser plugin for this request. Defaults to true. |
| `Pdf`                                                                              | [*components.PDFParserOptions](../../models/components/pdfparseroptions.md)        | :heavy_minus_sign:                                                                 | Options for PDF parsing.                                                           |