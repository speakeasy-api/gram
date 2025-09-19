---
title: Toolsets
description: Organize your agentic tools into cohesive sets that help agents solve specific problems
sidebar:
  order: 1
---

Giving an LLM access to too many tools can exhaust the context window, which may prevent agents from functioning properly or cause the LLM to choose the wrong tools.

To avoid exhausting the context window, Gram lets you organize tool definitions into curated collections called **toolsets**. Each toolset is designed to support a specific goal or use case. 

For example, you might create a toolset to identify inactive customers and send them promotional coupons. It could include one tool to retrieve customer data and another to send emails. These tools might come from different APIs but can be combined in a single toolset for a unified workflow.

You can also use toolsets to scope separate toolsets for different teams, for example, sales, marketing, and support. This ensures agents only have access to the tools relevant to their workflows, reducing confusion and improving LLM performance.

![Toolsets](/img/concepts/toolsets/toolsets-listing.png)

:::tip[Good to know]
Certain language models place a hard cap on the total number of tools that can be passed to them in a chat completion call.
:::

## Designing effective toolsets

When creating a toolset, start by identifying the specific task you want an LLM client to perform using your API and enabled integrations, then include only the tools necessary to accomplish that task. A cohesive, task-focused toolset significantly increases the likelihood that an agent will interact with your API correctly and effectively. 

To ensure your toolsets are LLM-friendly:

- Refine tool names and descriptions using Gram's [tool-variations](/concepts/tool-variations) feature to improve clarity and usability for the LLM.
- Keep toolsets focused by including only what's needed for the task. Avoid unrelated tools that may confuse the agent.

## Experimenting with toolsets

The Gram Playground provides an interactive environment for testing your toolsets. Using the Chat feature, you can send natural language prompts and see how the LLM selects tools and handles responses in real time.

When your toolset is ready, you can deploy it as an MCP server or integrate it directly into your agents for production use.


## Toolsets as MCP servers

In addition to enabling powerful agents through well-scoped toolsets, Gram automatically exposes each toolset as an MCP server using the streamable HTTP transport. Under the MCP tab for a toolset, you can view and retrieve the MCP server configuration.

![MCP server configuration](/img/concepts/toolsets/mcp-server-configuration.png)

You can also scroll down to the **Visibility** section to set the MCP server to public or private.

Under the the **MCP Installation** section, you can choose to install the MCP server using Pass-through authentication or Gram authentication.

![MCP server configuration](/img/concepts/toolsets/mcp-server-toolsets-config-options.png)

## Custom domains

MCP servers can be hosted through a custom subdomain such as `{your_sub_domain}/mcp`.

Contact Gram support to enable a custom subdomain for your account, then go to **Settings** in the Gram dashboard to complete the setup. 

You'll need to create two DNS entries:

1. A CNAME record pointing to `cname.getgram.ai.`
2. A TXT record named `_gram.{your_sub_domain}` with the value `gram-domain-verify={your_sub_domain},{gram_organization_id}`.

Once both DNS records have been created, contact Gram to complete the domain linking process.
