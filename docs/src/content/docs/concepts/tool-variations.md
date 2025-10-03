---
title: Tool variations
description: Prompt-engineer tool names and descriptions for better results
sidebar:
  order: 2
---

Endpoints listed in an OpenAPI document may lack clear summaries and descriptions that convey their intent and use. When a tool definition inherits an incomplete or vague description, LLMs may struggle to understand when to invoke the tool, how to use it properly, or how to interpret its output. 

Improving tool descriptions is a form of prompt engineering, and it's a critical step in building reliable, effective agents.

Gram provides two ways to improve a tool's description:

- [Editing the OpenAPI document](/concepts/openapi#using-the-x-gram-extension) and adding the `x-gram` extension to the endpoint to override or enrich the tool's metadata.
- Using the Gram Playground to directly edit a tool's name and description. This feature is called **Tool variations**.

Under the **Tools** tab for a toolset, you can edit the name and description of a tool.

![Editing a tool name](/img/concepts/tool-variations/editing-tool-name.png)

Update the tool name in the modal that opens:

![Validating tool name editing](/img/concepts/tool-variations/validating-tool-name-editing.png)

Similarly, hover over the tool description and click **Edit** to update it:

![Editing a tool description](/img/concepts/tool-variations/editing-tool-description.png)

Use the dedicated modal to validate and save your updated description:

![Validating tool description editing](/img/concepts/tool-variations/validating-tool-description-editing.png)
