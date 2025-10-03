---
title: Create a default toolset
description: Generate MCP server tools from OpenAPI on the Gram platform.
sidebar:
  order: 0
---

Gram uses your API as the starting point for the creation of your MCP server. A toolset will be generated with one tool for every API method (`GET`, `POST`, `PUT`, `DELETE`) in your API. This will be a the default toolset.

This toolset is created via an uploaded OpenAPI document.

The default toolset will likely contain too many tools for an LLM to use effectively in production. Before launching the MCP server, [a custom toolset will be curated](/) to contain only the tools that are relevant to the intended use case.

## Generating tools from OpenAPI

Gram converts OpenAPI endpoints into tool definitions that MCP clients can understand and use.

To upload an OpenAPI document:

- In the Gram dashboard, click **Toolsets** in the sidebar (under **Create**).
- Click **+ Add API**.

![Upload OpenAPI document](/img/guides/build-mcp/uploading-openapi-document.png)

- Select the OpenAPI document to upload and add the API name.

![Validating tool definitions](/img/guides/build-mcp/01-upload-openapi-document-done.png)

Gram will parse the uploaded OpenAPI document and generate [tool definitions](/concepts/tool-definitions) for each endpoint method.

## How it works

Gram does not make use of the entire OpenAPI document, therefore the document does not need to be 100% valid in order to successfully generate tool definitions.

In particular, Gram will pull out the URL paths, HTTP methods, request schemas, and operation descriptions.

Most OpenAPI documents, do not contain detailed, contextual descriptions. That's okay. Context can be added via [prompts](/build-mcp/writing-prompts) in the Gram or added to the OpenAPI document with the `x-gram` extension](/concepts/openapi#using-the-x-gram-extension) to help LLM agents understand and use your tools correctly.
