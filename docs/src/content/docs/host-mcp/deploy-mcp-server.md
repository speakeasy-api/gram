---
title: Deploy an MCP server
description: Deploying a hosted MCP server using Gram
sidebar:
  order: 1
---

Gram automatically exposes each of your toolsets as a hosted MCP server. You don't need to manage infrastructure, scaling, or versioning.

Here are the steps to deploy an MCP server using Gram:

## Generate a Gram API key

In **Settings**, click **New API Key**. Give the API key a name, and click **Create**.

![Creating an API key](/img/guides/build-mcp/04-adding-api-key.png)

## Get the MCP server configuration

In the sidebar, under **Consume**, click the **MCP** tab to view the MCP servers you have created.

![MCP servers](/img/guides/build-mcp/04-mcp-servers.png)

Click the MCP server you want to configure. This will take you to the MCP server configuration page.

![MCP server configuration](/img/guides/build-mcp/04-mcp-details.png)

Here you can see your hosted MCP server details. You can also change the custom slug for the MCP server and set a custom domain. 

If you scroll down to the **Visibility** section, you can change the visibility of the MCP server. You can make it public or private. 

![MCP server visibility](/img/guides/build-mcp/04-mcp-visibility.png)

Under the **MCP Installation** section, you can see the MCP server installation details. You can also copy the MCP server configuration to your MCP client and choose to use the MCP server with or without authentication. 

## Configure an MCP client

- Copy the MCP server configuration from Gram. Here's an example of what it looks like:

```json
{
    "mcpServers": {
      "GramShouldIPush": {
        "command": "npx",
        "args": [
          "mcp-remote",
          "https://app.getgram.ai/mcp/default/should-i-push/default",
          "--header",
          "Authorization: ${GRAM_KEY}"
        ],
        "env": {
          "GRAM_KEY": "Bearer <your-key-here>"
        }
      }
    }
}
```

- Replace `<your-key-here>` with your Gram API key.
- Add this configuration to the MCP client's configuration file.

## Next steps

Your MCP server is now live. Here are some next steps to consider:

- **Iterate on tool descriptions:** Refine tool definitions and use [tool variations](/concepts/tool-variations) to help LLMs better understand and invoke them accurately.
- **Create custom toolsets:** Group tools by task to design structured, step-by-step workflows tailored to specific use cases.
- **Integrate with frameworks:** Use the Gram Python or TypeScript SDK to build agentic workflows. On the **Playground** page, you can select a ready-to-use code snippet in [Python](https://pypi.org/project/gram-ai/) or [JavaScript](https://www.npmjs.com/package/@gram-ai/sdk).

## Troubleshooting

Common MCP server issues and fixes:

### Tools not working?

- Check your authentication configuration.
- Verify your server URL is accessible.

### Wrong tools being selected?

- Reduce the number of tools in your toolset.
- Use custom tools to create workflows with precise steps.
- Create focused toolsets for different use cases.

