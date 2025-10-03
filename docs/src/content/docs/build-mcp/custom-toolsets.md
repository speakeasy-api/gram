---
title: Curate a custom toolset
description: Curate tools into toolsets for production-ready MCP servers on the Gram platform.
sidebar:
  order: 1
---

Gram allows you to organize your tools into fit-for-purpose toolsets for improved performance with LLMs.

Tool curation is an important aspect of building a production-ready MCP server. LLMs work best when they have a clear understanding of the tools relevant to the intended use case. By curating a toolset, you can ensure that the LLMs avoid context overload and tool confusion.

We've included best practices for curating toolsets below.

To create a toolset:

- In the Gram dashboard, click **Toolsets** in the sidebar (under **Create**).
- Click **+ New Toolset**.

![Creating a toolset](/img/guides/build-mcp/02-adding-toolsets.png)

- Give the toolset a descriptive name.

![Naming a toolset](/img/guides/build-mcp/02-naming-toolset.png)

- When you click **Create**, you'll be redirected to the toolset configuration page with a list of all available tools. You can select the tools you want to include in the toolset or use the **Enable All** button to include all available tools in the set.

![Selecting tools](/img/guides/build-mcp/02-selecting-tools.png)

## Best practices for curating toolsets

:::tip[The art of tool curation]
If you want to go deeper in this topic, see [the art of tool curation](/build-mcp/advanced-tool-curation).
:::

- **Design workflow-first** - Group tools by agent tasks, not API structure. Create "Deal Creation" or "Deal Management" toolsets rather than mixing all CRUD operations together.

- **Include dependency tools** - Map tool dependencies and include prerequisite tools. If `create_deal` needs user IDs, include `search_users` in the same toolset.

- **Support progressive disclosure** - Include discovery tools (`search_users`), validation tools (`get_deal_by_id`), and action tools (`create_deal`) that work together naturally.

- **Create persona-specific toolsets** - Design different toolsets for different roles: sales reps need deal progression tools, managers need reporting tools, support needs customer issue tools.

- **Avoid the "everything" toolset** - Don't include every available tool in one set. This overwhelms agents and defeats the purpose of curation.

- **Test with real scenarios** - Use the Gram playground to validate your toolset with end-to-end workflows and watch how agents navigate your tools.

- **Maintain consistent naming** - Stick to patterns like `search_*` rather than mixing `search_users` and `find_companies` within the same toolset.
