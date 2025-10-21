---
title: Tool Sources
description: How Gram uses OpenAPI documents and Functions to generate tools.
sidebar:
  order: 4
---

**Sources** are the starting point for creating tools that can be utilized by
AI agents via MCP servers. A source is any input that describes available
functionality, such as:

- Gram Functions (Node.js), and
- OpenAPI documents.

These sources can be uploaded to Gram via the web UI, or using the [Gram
CLI](/reference/gram-cli). Once uploaded, a deployment creates tool
definitions. These tools can then be curated into **toolsets**, which are
organized collections tailored to specific use cases or workflows. Each toolset
is exposed as an **MCP server**, which can be installed into LLM clients for
automated usage.

## Gram Functions

Gram Functions are snippets of code written in TypeScript that enable you to
define arbitrary tasks for AI agents to execute via MCP servers. Unlike
OpenAPI-based tools that map to existing REST API endpoints, Gram Functions can
call multiple APIs, connect to remote databases over TCP/HTTP, and perform
complex data transformations with third-party libraries.

Functions are particularly useful when:

- You need to perform calculations, data transformations, or complex business
  logic that doesn't map to a single API endpoint.
- You want to orchestrate multiple API calls within a single tool to create
  workflow-based operations.
- You need to integrate third-party services or databases that don't have
  OpenAPI documents.
- You want to implement conditional logic, iteration, or other control flow
  that can't be expressed in a declarative API specification.

### Basic Structure

At its core, a Gram Function is a zip file containing two files:

**`manifest.json`** - A metadata file describing your tools:

```json
// manifest.json
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

The manifest includes each tool's name, description, JSON Schema for input
validation, and any required environment variables.

**`functions.js`** - A bundled JavaScript file that exports a `handleToolCall` function:

```javascript
// functions.js
export async function handleToolCall({ name, input }) {
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

When you upload this zip file to Gram, the platform uses the manifest to expose
your tools and invokes `handleToolCall` to execute them.

:::tip[NOTE]
Gram Functions support TypeSript as well as JavaScript.
:::

:::note[Bundle Size Limit]

The zipped bundle (containing both `manifest.json` and `functions.js`) must not
exceed **700KB**. Keep your functions lean by avoiding large dependencies and
using tree-shaking when bundling. For many use cases, this provides a lot of
headroom, especially when using the highest zip compression:

```bash
zip -9 gram.zip manifest.json functions.js
```

:::

### Environment Variables

Environment variables defined in your Gram project can be accessed within
functions using the `process.env` object. For example, if you define an
environment variable named `MY_MCP_MULTIPLIER`, you can access it in a function
like this:

```javascript 'process.env.MY_MCP_MULTIPLIER'
// functions.js
export async function handleToolCall({ name, input }) {
  switch (name) {
    case "multiply":
      return json({ value: input.a * process.env.MY_MCP_MULTIPLIER ) });
    // other cases...
    default:
      throw new Error(`Unknown tool: ${name}`);
  }
}
```

### Using the SDK

While you can manually create these files, the
[`@gram-ai/functions`](https://github.com/speakeasy-api/gram/tree/main/ts-framework/create-function/gram-template-gram)
TypeScript framework simplifies the entire workflow. The SDK handles manifest
generation, bundling, input validation, and packaging automatically.

To create a new function project using the SDK, use the scaffolding tool:

```bash
pnpm create @gram-ai/function@latest --template gram
```

Here's a basic example using the SDK:

```typescript
// src/functions.ts
import { Gram } from "@gram-ai/functions";
import * as z from "zod/mini";

const g = new Gram().tool({
  name: "calculate_discount",
  description: "Calculate the final price after applying a discount percentage",
  inputSchema: {
    originalPrice: z.number().positive(),
    discountPercent: z.number().min(0).max(100),
  },
  async execute(ctx, input) {
    const discount = input.originalPrice * (input.discountPercent / 100);
    const finalPrice = input.originalPrice - discount;

    return ctx.json({
      originalPrice: input.originalPrice,
      discount: discount,
      finalPrice: finalPrice,
    });
  },
});

export const handleToolCall = g.handleToolCall;
```

The SDK automatically generates the manifest and bundles your code when you run
`pnpm build`.

For more details on using the SDK to create and deploy Gram Functions, check
out the framework's [GitHub
Repository](https://github.com/speakeasy-api/gram/tree/main/ts-framework/create-function/gram-template-gram).

## OpenAPI Documents

OpenAPI documents describe the functionality of REST APIs in a standardized
format known as the OpenAPI Specification. These files are widely used to
generate API documentation, SDKs, and client libraries. Similaraly, Gram
leverages OpenAPI documents to generate tools that enable LLMs to interact with
REST APIs. OpenAPI-sourced tools are especially useful when:

- You want to make it easy for end-users to leverage your REST API via AI
  agents.
- You want to automate workflows that involve multiple API calls.
- You want to enhance LLMs with real-time data and functionality from your API.

Though most users upload these documents for their own REST APIs, _tools may be
generated using an OpenAPI document for any API_. Some examples of public APIs
that have OpenAPI documents include:

<table class="w-full table-auto">
  <thead>
    <tr>
      <th>API</th>
      <th>Documentation</th>
      <th>OpenAPI Document</th>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td>Asana</td>
      <td><a href="https://developers.asana.com/reference/rest-api-reference">developers.asana.com</a></td>
      <td><a href="https://raw.githubusercontent.com/Asana/openapi/master/defs/asana_oas.yaml">asana_oas.yaml</a></td>
    </tr>
    <tr>
      <td>GitHub REST API</td>
      <td><a href="https://docs.github.com/en/rest">docs.github.com/en/rest</a></td>
      <td><a href="https://raw.githubusercontent.com/github/rest-api-description/main/descriptions/api.github.com/api.github.com.yaml">api.github.com.yaml</a></td>
    </tr>
    <tr>
      <td>National Weather Service</td>
      <td><a href="https://www.weather.gov/documentation/services-web-api">weather.gov/documentation/services-web-api</a></td>
      <td><a href="https://api.weather.gov/openapi.json">openapi.json</a></td>
    </tr>
  </tbody>
</table>

:::note[NOTE]
Gram works best with documents using [OpenAPI
3.1.x](https://spec.openapis.org/oas/v3.1.1) and its corresponding JSON Schema
version. See the section on [Limitations of OpenAPI
3.0.x](#limitations-of-openapi-30x) for more details.
:::

### Optimizing OpenAPI Documents 

Because Gram generates tools directly from endpoint descriptions in your OpenAPI document, it's essential that those descriptions are accurate and informative. However, writing descriptions that serve both humans and LLMs can be challenging.

Short descriptions may be readable for humans, but LLMs often require more context to interpret intent and usage correctly. To bridge this gap, Gram supports the `x-gram` extension in OpenAPI documents, allowing you to provide LLM-optimized metadata specifically for tool generation and usage.

```yaml {8,9,22-33}
openapi: 3.1.0
info:
  title: E-commerce API
  version: 1.0.0
paths:
  /products/{merchant_id}/{product_id}:
    get:
      summary: Get a product
      operationId: E-Commerce V1 / product
      tags: [ecommerce]
      parameters:
        - name: merchant_id
          in: path
          required: true
          schema:
            type: string
        - name: product_id
          in: path
          required: true
          schema:
            type: string
      x-gram:
        name: get_product
        summary: ""
        description: |
          <context>
            This endpoint returns details about a product for a given merchant.
          </context>
          <prerequisites>
            - If you are presented with a product or merchant slug then you must first resolve these to their respective IDs.
            - Given a merchant slug use the `resolve_merchant_id` tool to get the merchant ID.
            - Given a product slug use the `resolve_product_id` tool to get the product ID.
          </prerequisites>
        responseFilterType: jq
      responses:
        "200":
          description: Details about a product
          content:
            application/json:
              schema:
                $ref: "#/components/schemas/Product"
```

Without the `x-gram` extension, the generated tool would be named `ecommerce_e_commerce_v1_product`, and have the description `"Get a product by its ID"`, resulting in a poor quality tool. The `x-gram` extension allows you to customize a tool's name and description without altering the original information in the OpenAPI document.

The `x-gram` extension also supports [response filtering](/build-mcp/response-filtering) through the `responseFilterType` property, which helps LLMs process API responses more effectively.

Using the `x-gram` extension is optional. With Gram's [tool variations](/concepts/tool-variations) feature, you can modify a tool's name and description when curating tools into toolsets. However, it might be worth using the `x-gram` extension to make your OpenAPI document clean, descriptive, and LLM-ready before bringing it into Gram, so your team doesn't need to fix tool names and descriptions later.

### Limitations of OpenAPI 3.0.x

Many LLMs don't support the JSON Schema version used in OpenAPI 3.0.x documents. When these documents are uploaded to Gram, they are transparently upgraded to 3.1.0 using the steps defined in [Migrating from OpenAPI 3.0 to 3.1.0](https://www.openapis.org/blog/2021/02/16/migrating-from-openapi-3-0-to-3-1-0). When this happens you might notice that line numbers no longer match the original OpenAPI document. It's recommended to upgrade your OpenAPI documents to 3.1.x to have a more streamlined experience.

:::tip[OpenAPI Resources]
If you are looking for more information on how to write, understand and manage OpenAPI documents we reccomend checking out [Speakeasy's documentation site on OpenAPI](https://www.speakeasy.com/openapi).

Speakeasy also provides a comprehensive OpenAPI Editor and CLI that help you edit, save and lint OpenAPI documents. You can login to Speakeasy [here](https://app.speakeasy.com) using the same credientials used to access the Gram platform.
:::


