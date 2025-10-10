---
title: Tool call healing
description: Automatic validation and repair of tool call parameters
sidebar:
  order: 5
---

LLMs sometimes form tool call parameters incorrectly even in the presence of a valid JSON schema. Gram provides built-in tool call "healing" to improve reliability when LLMs make tool calls with invalid parameters. This happens automatically without additional configuration.

## Features

### JSON Schema Validation

All tool call parameters are automatically validated against the JSON schema defined in the tool's input schema. If parameters don't match the expected types, formats, or constraints, the validation will catch these errors before the tool is executed. We then return a helpful message to the LLM describing the errors in the provided input. Typically this enables the LLM to self-heal, resulting in a successful tool call when it retries its request.

### Invalid JSON Healing

When an LLM generates malformed JSON in tool call parameters, Gram attempts to automatically repair the JSON using best-effort parsing. Certain models can be unreliable at producing complex JSON from a provided JSON schema, often producing "stringified" JSON for nested objects. If passed directly to your API, these request bodies would cause JSON parsing errors. To help avoid this, Gram will recurse over the tool call parameters when invalid parameters are detected and attempt to massage the provided parameters into a form that matches the required schema. For example, when a value is provided as a string but is expected to be an object, Gram will detect if that string contains a serialized JSON object matching the request schema and "heal" it if so.

**Before healing**
```json
{
  "name": "get_weather",
  "input": "{\"lat\": 123, \"lng\": 456}"
}
```
**After healing**
```json
{
  "name": "get_weather",
  "input": { "lat": 123, "lng": 456 }
}
```

## Benefits

Tool call healing reduces failed tool executions and improves the overall reliability of MCP server integrations, leading to higher tool call success rates.
