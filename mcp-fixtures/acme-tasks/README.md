# Acme Tasks

A small task-tracker MCP server built with [Speakeasy Functions](https://www.speakeasy.com/docs/ai-control-plane/mcp-gateway/building-servers/functions). Its tools return fixed sample data and never call out or persist anything, so every call gives the same answer.

We deploy it to a Speakeasy-owned organization as a **private** MCP server, which means connecting to it needs a Speakeasy sign-in. That makes it a stable OAuth upstream for:

- the OpenAI app review test cases, where reviewers add it to their review organization and connect it without any third-party account, and
- production integration testing and monitoring.

## Tools

| Tool           | What it does                                           |
| -------------- | ------------------------------------------------------ |
| `list_tasks`   | Lists tasks, optionally filtered by status or assignee |
| `get_task`     | Gets one task by ID, for example `ACME-101`            |
| `search_tasks` | Finds tasks by words in their title or labels          |

All three are read-only.

## Develop

```sh
npm install --no-package-lock
npm run lint
npm test
npm run dev   # MCP Inspector over stdio
```

## Deploy

```sh
speakeasy functions push
```

Then, in the Speakeasy dashboard, create an MCP server from these tools and keep it private.
