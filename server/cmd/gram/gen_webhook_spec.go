package gram

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/urfave/cli/v2"
	"gopkg.in/yaml.v3"

	"github.com/speakeasy-api/gram/server/internal/outbox/events"
)

func newGenWebhookSpecCommand() *cli.Command {
	return &cli.Command{
		Name:  "gen-webhook-spec",
		Usage: "Emit an OpenAPI 3.1 document cataloging all webhook event types to stdout",
		Action: func(c *cli.Context) error {
			webhooks := make(map[string]any, len(events.All))
			for _, ev := range events.All {
				var schemaNode any
				if err := json.Unmarshal(ev.JSONSchema(), &schemaNode); err != nil {
					return fmt.Errorf("unmarshal schema for event %q: %w", ev.EventType(), err)
				}

				post := map[string]any{
					"operationId": string(ev.EventType()),
					"description": ev.Description(),
					"requestBody": map[string]any{
						"required": true,
						"content": map[string]any{
							"application/json": map[string]any{
								"schema": schemaNode,
							},
						},
					},
				}
				if ev.FeatureFlag() != "" {
					post["x-svix-feature-flag"] = ev.FeatureFlag()
				}
				if ev.GroupName() != "" {
					post["x-svix-group-name"] = ev.GroupName()
				}
				webhooks[string(ev.EventType())] = map[string]any{
					"post": post,
				}
			}

			doc := map[string]any{
				"openapi": "3.1.0",
				"info": map[string]any{
					"title":   "Speakeasy API Control Plane Webhook Events",
					"version": "0.0.0",
				},
				"webhooks": webhooks,
			}

			bs, err := yaml.Marshal(doc)
			if err != nil {
				return fmt.Errorf("marshal webhook spec: %w", err)
			}
			if _, err = os.Stdout.Write(bs); err != nil {
				return fmt.Errorf("write webhook spec: %w", err)
			}
			return nil
		},
	}
}
