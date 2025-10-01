---
title: Tool definitions
description: Mapping your HTTP API to tools that agents can use
sidebar:
  order: 0
---

When you upload an [OpenAPI document](/concepts/openapi) or enable an integration in Gram, Gram creates a deployment and begins processing it. During this stage, every operation defined in the OpenAPI document is automatically converted into a corresponding tool definition.

![Generating tools](/img/concepts/tool-definitions/tools-generation.png)

Tool definitions contain both the metadata needed to describe a tool to an LLM and the configuration Gram uses to construct the corresponding HTTP request to your API or a third-party integration.

When creating toolsets, you select the relevant tool definitions to include and invoke them using the Gram Playground, SDK, or MCP server. To build and proxy the HTTP request to the appropriate endpoint, Gram combines the tool definition with the selected [environment](/concepts/environments) (which defaults to `Default Env`).
