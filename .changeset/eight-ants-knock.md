---
"@gram/server": minor
---

Introducing support for Gram Functions as part of deployments. As part of
deployment processing, each function attached to a deployment will have a Fly.io
app created for it which will eventually receive tool calls from the Gram
server.

Gram Functions are serverless functions that are exposed as LLM tools to be used
in your toolsets and MCP servers. They can execute any arbitrary code and make
the result available to LLMs. This allows you to go far beyond what is possible
with today's OpenAPI artifacts alone

At its code, a Gram Function is zip file containing at least two files:
`manifest.json` and `functions.ts`.

This is a JSON file describing the tools including their names, descriptions,
input schemas and any environment variables they require. For example:

```json
{
  "version": "0.0.0",
  "tools": [
    {
      "name": "add",
      "description": "Add two numbers",
      "inputSchema": {
        "type": "object",
        "properties": {
          "a": { "type": "number" },
          "b": { "type": "number" }
        },
        "required": ["a", "b"]
      }
    },
    {
      "name": "square_root",
      "description": "Calculate the square root of a number",
      "inputSchema": {
        "type": "object",
        "properties": {
          "a": { "type": "number" }
        },
        "required": ["a"]
      }
    }
  ]
}
```

A JavaScript or TypeScript file exporting the actual function implementation for
tool calls. Here's a function that implements the manifest above:

```javascript
function json(value: unknown) {
  return new Response(JSON.stringify(value), {
    headers: { "Content-Type": "application/json" },
  });
}

export async function handleToolCall({ name, input }) {
  // process.env will also containe any environment variables passed on from
  // Gram.

  switch (name) {
    case "add":
      return json({ value: input.a + input.b });
    case "square_root":
      return json({ value: Math.sqrt(input.a) });
    default:
      throw new Error(`Unknown tool: ${name}`);
  }
}
```

Notably:

- The file must export an async function called `handleToolCall` which takes
  the tool name and input object as parameters.
- This function must return a `Response` object.
- You can use any npm packages you like but you must ensure they are included in
  the zip file.

- We currently only support TypeScript/JavaScript functions and deploy them into
  small Firecracker microVMs running Node.js v22.
- Each function zip file must be a little under 750KiB in size or less than 1MiB
  when encoded in base64.
- Third-party dependencies are supported but you must decide how to include in
  zip archives. You may bundle everything into a single file or include a
  `package.json` and node_modules directory in the zip file. As long as the total
  size is under the limit, it should work.
- The code will be deployed into `/var/task` in the microVM.
- The code will only have permission to write to `/tmp`.
- The code must not depend on data persisting to disk between successive tool
  calls.
