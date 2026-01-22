# Beta.Responses

## Overview

beta.responses endpoints

### Available Operations

* [Send](#send) - Create a response

## Send

Creates a streaming or non-streaming response using OpenResponses API format

### Example Usage

<!-- UsageSnippet language="go" operationID="createResponses" method="post" path="/responses" -->
```go
package main

import(
	"context"
	"github.com/speakeasy-api/gram/openrouter"
	"github.com/speakeasy-api/gram/openrouter/models/components"
	"github.com/speakeasy-api/gram/openrouter/optionalnullable"
	"log"
)

func main() {
    ctx := context.Background()

    s := openrouter.New(
        openrouter.WithSecurity("<YOUR_BEARER_TOKEN_HERE>"),
    )

    res, err := s.Beta.Responses.Send(ctx, components.OpenResponsesRequest{
        Input: openrouter.Pointer(components.CreateOpenResponsesInputArrayOfOpenResponsesInput1(
            []components.OpenResponsesInput1{
                components.CreateOpenResponsesInput1OpenResponsesEasyInputMessage(
                    components.OpenResponsesEasyInputMessage{
                        Type: components.OpenResponsesEasyInputMessageTypeMessageMessage.ToPointer(),
                        Role: components.CreateOpenResponsesEasyInputMessageRoleUnionOpenResponsesEasyInputMessageRoleUser(
                            components.OpenResponsesEasyInputMessageRoleUserUser,
                        ),
                        Content: components.CreateOpenResponsesEasyInputMessageContentUnion2Str(
                            "Hello, how are you?",
                        ),
                    },
                ),
            },
        )),
        Tools: []components.OpenResponsesRequestToolUnion{
            components.CreateOpenResponsesRequestToolUnionFunction(
                components.OpenResponsesRequestToolFunction{
                    Type: components.OpenResponsesRequestTypeFunction,
                    Name: "get_current_weather",
                    Description: optionalnullable.From(openrouter.Pointer("Get the current weather in a given location")),
                    Parameters: map[string]any{
                        "type": "object",
                        "properties": map[string]any{
                            "location": map[string]any{
                                "type": "string",
                            },
                        },
                    },
                },
            ),
        },
        Model: openrouter.Pointer("anthropic/claude-4.5-sonnet-20250929"),
        Temperature: optionalnullable.From(openrouter.Pointer[float64](0.7)),
        TopP: optionalnullable.From(openrouter.Pointer[float64](0.9)),
    })
    if err != nil {
        log.Fatal(err)
    }
    if res.OpenResponsesNonStreamingResponse != nil {
        defer res.Object.Close()

        for res.Object.Next() {
            event := res.Object.Value()
            log.Print(event)
            // Handle the event
	      }
    }
}
```

### Parameters

| Parameter                                                                          | Type                                                                               | Required                                                                           | Description                                                                        |
| ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- |
| `ctx`                                                                              | [context.Context](https://pkg.go.dev/context#Context)                              | :heavy_check_mark:                                                                 | The context to use for the request.                                                |
| `request`                                                                          | [components.OpenResponsesRequest](../../models/components/openresponsesrequest.md) | :heavy_check_mark:                                                                 | The request object to use for the request.                                         |
| `opts`                                                                             | [][operations.Option](../../models/operations/option.md)                           | :heavy_minus_sign:                                                                 | The options for this request.                                                      |

### Response

**[*operations.CreateResponsesResponse](../../models/operations/createresponsesresponse.md), error**

### Errors

| Error Type                                 | Status Code                                | Content Type                               |
| ------------------------------------------ | ------------------------------------------ | ------------------------------------------ |
| apierrors.BadRequestResponseError          | 400                                        | application/json                           |
| apierrors.UnauthorizedResponseError        | 401                                        | application/json                           |
| apierrors.PaymentRequiredResponseError     | 402                                        | application/json                           |
| apierrors.NotFoundResponseError            | 404                                        | application/json                           |
| apierrors.RequestTimeoutResponseError      | 408                                        | application/json                           |
| apierrors.PayloadTooLargeResponseError     | 413                                        | application/json                           |
| apierrors.UnprocessableEntityResponseError | 422                                        | application/json                           |
| apierrors.TooManyRequestsResponseError     | 429                                        | application/json                           |
| apierrors.InternalServerResponseError      | 500                                        | application/json                           |
| apierrors.BadGatewayResponseError          | 502                                        | application/json                           |
| apierrors.ServiceUnavailableResponseError  | 503                                        | application/json                           |
| apierrors.EdgeNetworkTimeoutResponseError  | 524                                        | application/json                           |
| apierrors.ProviderOverloadedResponseError  | 529                                        | application/json                           |
| apierrors.APIError                         | 4XX, 5XX                                   | \*/\*                                      |