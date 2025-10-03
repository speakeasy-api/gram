---
title: "Quickstart ⭐"
description: "Get started with Gram in three simple steps: create an MCP server, configure toolsets, and connect to AI agents."
sidebar:
  order: 2
---

## Creating and hosting an MCP server in three simple steps

Gram takes your OpenAPI document and, in just three steps, generates a fully functional, scoped MCP server, ready to use across environments, teams, and agents.

![Running MCP server with Gram](/img/blog/the-easiest-way-to-host-mcp-servers/gram-ai-hosting.png)

Expose your MCP server as an internal or public service without managing infrastructure, while Gram handles hosting, distribution, and security.

Let's see how easy it is to create an MCP server with Gram.

### Step 1: Create an MCP server

First, upload an OpenAPI document.

:::tip[NOTE]
The quality of your OpenAPI document directly impacts the quality of your MCP server. Learn about writing better OpenAPI documents in the [OpenAPI hub](https://www.speakeasy.com/openapi).
:::

<video width="600" controls>
  <source src="/img/blog/concepts/add-openapi-spec.mp4" type="video/mp4" />
  Your browser does not support the video tag.
</video>

### Step 2: Create a toolset

A [toolset](https://docs.getgram.ai/concepts/toolsets) is a curated bundle of tools for a specific use case or team. In the Gram interface, you can select which tools to include and which to ignore based on your needs.

<video width="600" controls>
  <source src="/img/blog/concepts/creating-a-toolset.mp4" type="video/mp4" />
  Your browser does not support the video tag.
</video>

### Step 3: Set environment variables

Click **Environments** in the sidebar, and then **+ New Environment**.

<video width="600" controls>
  <source src="/img/blog/concepts/creating-environment.mp4" type="video/mp4" />
  Your browser does not support the video tag.
</video>

Now you can interact with your MCP server in real time in the Gram Playground.

![MCP Playground](/img/blog/the-easiest-way-to-host-mcp-servers/gram-playground.png)

## Public and authenticated servers

To configure your MCP server as public or authenticated, go to **MCP** in the sidebar and select **MCP Config** for your hosted server.

- A **public server** can be instantly used by any of your customers, simply by copying the configuration provided into an MCP client.

![Gram Public Server](/img/blog/the-easiest-way-to-host-mcp-servers/gram-public-server.png)

- An **authenticated server** requires a Gram API key in the project configuration, making it suitable for internal use cases where access needs to be restricted to authorized users.

![Gram Authenticated Server](/img/blog/the-easiest-way-to-host-mcp-servers/gram-authenticated-server.png)

## Connect your MCP server to AI agents

In addition to MCP Clients like Claude, your customers can automate interactions with your MCP server using AI agents. The Gram Python and TypeScript SDKs support OpenAI Agents, LangChain, and other function-based tooling, and Gram provides sample code to help you create agents within your existing framework.

Here is an example Python snippet that shows how to integrate your MCP server with LangChain:

```python
import asyncio
import os
from langchain import hub
from langchain_openai import ChatOpenAI
from langchain.agents import AgentExecutor, create_openai_functions_agent
from gram_ai.langchain import GramLangchain

key = "<GRAM_API_KEY>"

gram = GramLangchain(api_key=key)

llm = ChatOpenAI(
    model="gpt-4",
    temperature=0,
    openai_api_key=os.getenv("OPENAI_API_KEY")
)

tools = gram.tools(
    project="default",
    toolset="marketing",
    environment="demo-environment",
)

prompt = hub.pull("hwchase17/openai-functions-agent")

agent = create_openai_functions_agent(llm=llm, tools=tools, prompt=prompt)

agent_executor = AgentExecutor(agent=agent, tools=tools, verbose=False)

async def main():
    response = await agent_executor.ainvoke({
        "input": "Can you tell me about my tools?"
    })
    print(response)

if __name__ == "__main__":
    asyncio.run(main())
```

In the **Agents** tab in the Playground dashboard, you can build agentic workflows by selecting a language and integration type.

![Gram agents integration](/img/blog/the-easiest-way-to-host-mcp-servers/gram-agentic-workflow.png)

## What's next?

Gram also offers:

- API key management
- Custom domains for distribution
- Integrations with popular platforms like Slack and GitHub

[Try Gram now](https://getgram.ai/) and see how easy it is to host and share your MCP server.
