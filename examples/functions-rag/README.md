# Gram Functions: RAG Integration with Ragie

This example demonstrates real-world patterns for building [Gram
Functions](https://www.speakeasy.com/docs/gram/gram-functions/introduction) that
implement RAG (Retrieval-Augmented Generation) workflows. Study this project to
learn how to structure document management and semantic search tools with proper
API integration, validation, and data transformation.

## What's Included

**Document Management Tools**

- `list_partitions` - Lists document partitions in your Ragie account. Demonstrates filtering and pagination patterns.
- `upload_file` - Uploads documents from local file paths. Demonstrates file handling and blob operations.
- `upload_url` - Ingests documents from URLs. Demonstrates asynchronous document processing.
- `upload_text` - Creates documents from raw text. Demonstrates programmatic content indexing.

**Search and Retrieval Tools**

- `search` - Performs semantic RAG search across indexed documents. Demonstrates vector search integration with configurable ranking.
- `fetch` - Downloads document content by ID. Demonstrates content retrieval with metadata extraction.

## Key Patterns

- **Type-safe validation** with Zod schemas for all inputs
- **Environment variable management** for secure API key handling
- **External API integration** using the Ragie SDK
- **Document lifecycle workflows** from upload to search to retrieval
- **RAG search implementation** with ranking and filtering capabilities
- **Metadata handling** for document organization and tracking

## Quick start

To get started, install dependencies and run the development server:

```bash
npm install
```

To build a zip file that can be deployed to Gram, run:

```bash
npm build
```

After building, push your function to Gram with:

```bash
npm push
```

## Testing Locally

If you want to poke at the tools you've built during local development, you can
start a local MCP server over stdio transport with:

```bash
npm dev
```

Specifically, this command will spin up [MCP inspector][mcp-inspector] to let
you interactively test your tools.

[mcp-inspector]: https://github.com/modelcontextprotocol/inspector

## Learn More

- [Gram Functions Documentation](https://www.speakeasy.com/docs/gram/gram-functions/introduction)
- [Framework API Reference](./CONTRIBUTING.md)
