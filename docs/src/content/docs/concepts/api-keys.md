---
title: API keys
description: How to create a Gram API key
sidebar:
  order: 7
---

Gram API keys are used to authenticate requests to the Gram API and can be used with MCP server configurations or Gram SDKs. There are two types of API keys: Consumer keys and Producer keys.

### Consumer keys

Consumer keys are for accessing Gram servers with managed authentication. Use these when connecting MCP clients to Gram servers or when you need to query data from existing toolsets.

### Producer keys

Producer keys are for accessing Gram platform capabilities, like spec updates and toolset management. Use these when you need to programmatically create or modify toolsets, upload OpenAPI documents, or trigger deployments.

### Creating an API key

To generate a Gram API key, navigate to the **Settings** page of the Gram dashboard and click on the **New API Key** button. Select the appropriate scope based on whether you need Consumer or Producer access.

![Creating an API key](/img/concepts/api-keys/adding-api-key.png)

Copy the API key and follow your MCP client's instructions to add the configuration to your client.  

If you're using the Gram SDKs, pass the API key as an environment variable to authenticate requests programmatically.

```py title="openai-agents-example.py" {6}
import asyncio
import os
from agents import Agent, Runner, set_default_openai_key
from gram_ai.openai_agents import GramOpenAIAgents

key = os.getenv("GRAM_API_KEY")

gram = GramOpenAIAgents(api_key=key)

set_default_openai_key(os.getenv("OPENAI_API_KEY"))

# ...
```

:::tip[Best practice]
We recommend always using the Gram API key as an environment variable for security and flexibility.

To isolate access and improve traceability, use separate API keys for each agent or client configuration, especially when setting up MCP server connections.
:::
