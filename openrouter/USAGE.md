<!-- Start SDK Example Usage [usage] -->
```go
package main

import (
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
					Type:        components.OpenResponsesRequestTypeFunction,
					Name:        "get_current_weather",
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
		Model:       openrouter.Pointer("anthropic/claude-4.5-sonnet-20250929"),
		Temperature: optionalnullable.From(openrouter.Pointer[float64](0.7)),
		TopP:        optionalnullable.From(openrouter.Pointer[float64](0.9)),
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
<!-- End SDK Example Usage [usage] -->