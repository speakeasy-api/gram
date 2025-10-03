---
title: OpenAPI
description: Document your API with OpenAPI to generate tools with Gram
sidebar:
  order: 4
---

Gram uses OpenAPI documents to generate tool definitions. The OpenAPI Specification is a powerful standard for describing APIs. Documents based on this standard can be used to [generate documentation and SDKs](https://speakeasy.com), and Gram uses them to generate tools directly from their endpoint definitions.

For best results, we recommend using [OpenAPI 3.1.x](https://spec.openapis.org/oas/v3.1.1) and its corresponding JSON Schema version to describe your API.

## Using the `x-gram` extension

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

## Limitations of OpenAPI 3.0.x

Many LLMs don't support the JSON Schema version used in OpenAPI 3.0.x documents. When these documents are uploaded to Gram, they are transparently upgraded to 3.1.0 using the steps defined in [Migrating from OpenAPI 3.0 to 3.1.0](https://www.openapis.org/blog/2021/02/16/migrating-from-openapi-3-0-to-3-1-0). When this happens you might notice that line numbers no longer match the original OpenAPI document. It's recommended to upgrade your OpenAPI documents to 3.1.x to have a more streamlined experience.

:::tip[OpenAPI Resources]
If you are looking for more information on how to write, understand and manage OpenAPI documents we reccomend checking out [Speakeasy's documentation site on OpenAPI](https://www.speakeasy.com/openapi).

Speakeasy also provides a comprehensive OpenAPI Editor and CLI that help you edit, save and lint OpenAPI documents. You can login to Speakeasy [here](https://app.speakeasy.com) using the same credientials used to access the Gram platform.
:::

