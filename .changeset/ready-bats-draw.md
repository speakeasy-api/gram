---
"@gram/server": patch
---

Introduce “healing” of invalid tool call arguments. For certain large tool input JSON schemas, LLMs can sometimes pass in stringified JSON where literal JSON is expected. We can unpack the correct json object out of this, even after the LLM mistake.

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
  "input": {"lat": 123, "lng": 456}
}
```
