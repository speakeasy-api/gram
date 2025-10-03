---
title: Environments
description: Manage authentication, authorization, and server details for an API
sidebar:
  order: 3
---

APIs typically require authentication, authorization, and a server URL before they can be accessed. In many cases, you’ll also need to manage multiple environments (such as **production** and **staging**) or support multi-tenant configurations.

With Gram, you can define multiple environments to manage different sets of credentials, server URLs, and other runtime variables. These environments can then be selected and applied when using the Gram SDKs in agentic workflows.

Additionally, Gram will automatically warn you if you attempt to use a toolset in the Playground without configuring the necessary environment variables, such as API keys, base URLs, or other required parameters.

![Gram warnings about missing environment variables](/img/concepts/environments/gram-warning-missing-variables.png)

## Creating environments

To create an environment, navigate to the **Environments** page, click **+ New Environment**, and enter a name for the environment.

![Creating an environment](/img/concepts/environments/creating-an-environment.png)

On the environment page, add environment variables by clicking **New Variable**.

![Adding environment variables](/img/concepts/environments/adding-environment-variables.png)

Attach an environment to a toolset by clicking the **Fill for toolset** button, then selecting the specific toolset you want to configure. This allows you to pre-fill all required environment variables for that toolset, ensuring it's ready to use across the Playground, SDKs, and MCP clients.

![Attaching an environment to a toolset](/img/concepts/environments/attaching-an-environment-to-a-toolset.png)

## Using environment variables with Gram SDKs

When initializing the Gram SDKs, specify which environment to use. The tools passed to the LLM will be bound to that environment.

```ts title="vercel-example.ts" {15}
import { generateText } from "ai";
import { VercelAdapter } from "@gram-ai/sdk/vercel";
import { createOpenAI } from "@ai-sdk/openai";

const key = process.env.GRAM_API_KEY;
const vercelAdapter = new VercelAdapter({ apiKey: key });

const openai = createOpenAI({
  apiKey: process.env.OPENAI_API_KEY,
});

const tools = await vercelAdapter.tools({
  project: "acme",
  toolset: "marketing",
  environment: "production",
});

const result = await generateText({
  model: openai("gpt-4"),
  tools,
  maxSteps: 5,
  prompt: "Can you tell me what tools you have available?",
});

console.log(result.text);
```

```py title="openai-agents-example.py" {17}
import asyncio
import os
from agents import Agent, Runner, set_default_openai_key
from gram_ai.openai_agents import GramOpenAIAgents

key = os.getenv("GRAM_API_KEY")

gram = GramOpenAIAgents(api_key=key)

set_default_openai_key(os.getenv("OPENAI_API_KEY"))

agent = Agent(
    name="Assistant",
    tools=gram.tools(
        project="acme",
        toolset="marketing",
        environment="production",
    ),
)


async def main():
    result = await Runner.run(
        agent,
        "Can you tell me what tools you have available?",
    )
    print(result.final_output)


if __name__ == "__main__":
    asyncio.run(main())
```

Environments are not required to use the Gram SDKs. You can pass the necessary variables directly when creating an instance. This is useful when users of your Gram toolsets prefer to use their own credentials.

```ts title="byo-env-vars.ts" {5-8}
import { VercelAdapter } from "@gram-ai/sdk/vercel";

const vercelAdapter = new VercelAdapter({
  apiKey: process.env.GRAM_API_KEY,
  environmentVariables: {
    ACME_API_KEY: process.env.ACME_API_KEY,
    ACME_SERVER_URL: "https://janesmith.acme.com",
  },
});
```

```py title="byo-env-vars.py" {6-9}
import os
from gram_ai.openai_agents import GramOpenAIAgents

gram = GramOpenAIAgents(
    api_key=os.getenv("GRAM_API_KEY"),
    environment_variables= {
        "ACME_API_KEY": os.getenv("ACME_API_KEY"),
        "ACME_SERVER_URL": "https://janesmith.acme.com",
    }
)
```

<div class="flex justify-center">
  <video controls>
    <source src="/videos/environments.mp4" type="video/mp4">
    Your browser does not support the video tag.
  </video>
</div>
