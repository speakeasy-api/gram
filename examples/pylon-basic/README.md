# Pylon Task Management with Gram Functions

This project provides Gram Functions for interacting with the [Pylon](https://usepylon.com) API, specifically for creating tasks.

Gram Functions are tools for LLMs and MCP servers that can do arbitrary tasks such as fetching data from APIs, performing calculations, or interacting with hosted databases.

## Available Tools

### `create_pylon_task`

Creates a new task in Pylon using the Pylon API.

**Parameters:**
- `title` (required): The title of the task
- `body_html` (optional): HTML content for the task body
- `status` (optional): Task status - one of `not_started`, `in_progress`, or `completed`
- `customer_portal_visible` (optional): Boolean to control visibility in customer portal
- `due_date` (optional): Due date in RFC 3339 format (e.g., `2025-12-31T23:59:59Z`)
- `assignee_id` (optional): ID of the user to assign the task to
- `account_id` (optional): ID of the associated account
- `project_id` (optional): ID of the associated project
- `milestone_id` (optional): ID of the associated milestone

**Environment Variables:**
- `PYLON_API_KEY`: Your Pylon API key for authentication

**Example Usage:**
```ts
{
  "title": "Review customer feedback",
  "body_html": "<p>Review and respond to customer feedback from last week</p>",
  "status": "not_started",
  "due_date": "2025-11-14T17:00:00Z",
  "customer_portal_visible": true
}
```

## Quick start

### 1. Install dependencies

```bash
pnpm install
```

### 2. Set up environment variables

Copy the example environment file and add your Pylon API key:

```bash
cp .env.example .env
```

Then edit `.env` and replace `your-pylon-api-key-here` with your actual Pylon API key.

You can get your Pylon API key from the [Pylon dashboard](https://app.usepylon.com).

### 3. Build and deploy

To build a zip file that can be deployed to Gram, run:

```bash
pnpm build
```

After building, push your function to Gram with:

```bash
pnpm push
```

### 4. Testing Locally

If you want to test the tools during local development, you can start a local MCP server over stdio transport with:

```bash
pnpm dev
```

Specifically, this command will spin up [MCP inspector][mcp-inspector] to let
you interactively test your tools.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector

## What next?

To learn more about using the framework, check out [CONTRIBUTING.md](./CONTRIBUTING.md)
