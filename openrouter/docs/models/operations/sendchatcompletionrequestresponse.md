# SendChatCompletionRequestResponse


## Fields

| Field                                                               | Type                                                                | Required                                                            | Description                                                         |
| ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- | ------------------------------------------------------------------- |
| `HTTPMeta`                                                          | [components.HTTPMetadata](../../models/components/httpmetadata.md)  | :heavy_check_mark:                                                  | N/A                                                                 |
| `ChatResponse`                                                      | [*components.ChatResponse](../../models/components/chatresponse.md) | :heavy_minus_sign:                                                  | Successful chat completion response                                 |
| `ChatStreamingResponseChunk`                                        | **stream.EventStream[components.ChatStreamingResponseChunk]*        | :heavy_minus_sign:                                                  | Successful chat completion response                                 |