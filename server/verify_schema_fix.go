package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/speakeasy-api/gram/server/gen/types"
	"github.com/speakeasy-api/gram/server/internal/conv"
)

func main() {
	// Create a test tool with a schema that includes $schema field
	// (simulating what comes from the database)
	testSchema := `{
		"$schema": "https://json-schema.org/draft/2020-12/schema",
		"type": "object",
		"properties": {
			"name": {
				"type": "string",
				"description": "Name of the person to greet"
			}
		},
		"required": ["name"]
	}`

	httpTool := &types.HTTPToolDefinition{
		ID:          "test-id",
		Name:        "greet",
		Description: "Greets a person by name",
		Schema:      testSchema,
	}

	tool := &types.Tool{
		HTTPToolDefinition: httpTool,
	}

	// Call ToToolListEntry (which applies our fix)
	name, description, inputSchema, _ := conv.ToToolListEntry(tool)

	fmt.Println("✓ Tool Name:", name)
	fmt.Println("✓ Tool Description:", description)
	fmt.Println("\n📋 Input Schema (should NOT contain $schema field):")
	fmt.Println(string(inputSchema))

	// Parse and verify
	var schemaMap map[string]interface{}
	if err := json.Unmarshal(inputSchema, &schemaMap); err != nil {
		fmt.Printf("\n❌ ERROR: Failed to parse schema: %v\n", err)
		os.Exit(1)
	}

	// Check if $schema field exists
	if _, hasSchema := schemaMap["$schema"]; hasSchema {
		fmt.Println("\n❌ FAIL: $schema field is still present in the output!")
		os.Exit(1)
	}

	// Verify important fields are preserved
	if schemaMap["type"] != "object" {
		fmt.Println("\n❌ FAIL: type field is missing or incorrect!")
		os.Exit(1)
	}

	if schemaMap["properties"] == nil {
		fmt.Println("\n❌ FAIL: properties field is missing!")
		os.Exit(1)
	}

	fmt.Println("\n✅ SUCCESS: $schema field has been stripped!")
	fmt.Println("✅ SUCCESS: All other fields are preserved!")
	fmt.Println("\n🎉 The fix is working correctly!")
}
