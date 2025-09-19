---
title: Troubleshooting
description: Overcome common issues when hosting and using MCP servers.
sidebar:
  order: 99 # last
---

Due to the emerging nature of the MCP specification and differences in how
various LLM providers and client applications implement it, you may encounter
issues when hosting and using MCP servers with Gram. This troubleshooting guide
addresses common problems and provides solutions to help you resolve them
quickly.

### Helpful Resources

The following external resources provide additional context and information
about MCP, and may be helpful when troubleshooting issues:

- <a href="https://modelcontextprotocol.io/specification/latest"
  target="_blank">MCP Specification.</a> The
  official documentation for the Model Context Protocol (MCP).
- <a href="https://modelcontextprotocol.io/clients" target="_blank">MCP Client
  Feature Support Matrix.</a> A list of well-known LLM clients and their level
  of MCP support. _Many LLM clients to do not support all of Gram MCP Server
  features_.

## Client Transport Closed Errors

### Using `mcp-remote`

Some LLM clients, such as **Claude**, are not yet able to connect to remote MCP
servers natively. To overcome this, the `mcp-remote` CLI tool is used to proxy
requests from the LLM client to hosted MCP servers:

```json ""mcp-remote""
{
  "mcpServers": {
    "remote-example": {
      "command": "npx",
      "args": [
        "mcp-remote",
        "https://remote.mcp.server/sse"
      ]
    }
  }
}
```

Since version `0.1.26` of the `mcp-remote` package, some users have <a
href="https://github.com/geelen/mcp-remote/issues/156"
target="blank">reported</a> servers failing to start, with `Client transport
closed` appearing in client logs.

This is typically due to the LLM client attempting to use an older version of
Node to execute `mcp-remote` than what is required.

#### Solutions

##### Use Node 20 or Higher (Recommended)

Ensure that you are using Node.js version 20 or higher. Instructions for
installing and managing Node.js versions can be found on the <a
href="https://nodejs.org/en/download/" target="_blank">official Node.js
website</a>.

##### Pin `mcp-remote` to Version `0.1.25`

If you're not able to install a compatible version of Node, you can pin the
`mcp-remote` package to version `0.1.25` in your MCP configuration.

```json ins="@0.1.25"
{
  "mcpServers": {
    "remote-example": {
      "command": "npx",
      "args": [
        "mcp-remote@0.1.25",
        "https://remote.mcp.server/sse"
      ]
    }
  }
}
```

## Getting Support

If you're encountering a persistent issue that is not covered in this
troubleshooting guide, please reach out to Gram support via the in-app chat, or
join our <a href="https://go.speakeasy.com/slack" target="_blank">Slack
community</a> to get help!
