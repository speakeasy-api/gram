# Gram Elements — Embedded Chat with Credential Passthrough

This example shows how to embed [Gram Elements](https://docs.getgram.ai) in a
[TanStack Start](https://tanstack.com/start) app, where the host app handles
authentication and passes the resulting credential to the Gram chat UI.

## Why this pattern?

When you deploy a hosted MCP server through Gram, that server often needs
credentials (e.g. a bearer token) to call upstream APIs on behalf of the user.
Rather than making the chat end-user obtain and manage that token themselves,
the embedding app can handle authentication and pass the credential to
`GramElementsProvider`. Gram forwards it to the MCP server as a header — the
end-user never sees or manages the token directly.

## How it works

```
┌──────────────┐     ┌──────────────────┐     ┌────────────────┐
│  Login Page  │────>│  /api/login       │────>│  localStorage  │
│  (/)         │     │  (returns token)  │     │  (stores token)│
└──────────────┘     └──────────────────┘     └───────┬────────┘
                                                      │
                                                      v
                                          ┌───────────────────────┐
                                          │  /chat                │
                                          │                       │
                                          │  GramElementsProvider │
                                          │    environment: {     │
                                          │      MY_MCP_BEARER_   │
                                          │      TOKEN: token     │
                                          │    }                  │
                                          └───────────┬───────────┘
                                                      │
                                                      v
                                          ┌───────────────────────┐
                                          │  MCP Server           │
                                          │  (receives token as   │
                                          │   a header)           │
                                          └───────────────────────┘
```

1. **Login** (`/`) — The user signs in. The app stores a token in
   `localStorage`. In a real app this would come from your auth provider
   (OAuth, session cookie, etc).

2. **Session endpoint** (`/api/chat/session`) — A server-side route that
   proxies to Gram's session API. This keeps `GRAM_API_KEY` on the server and
   never exposes it to the client.

3. **Chat** (`/chat`) — Reads the token from `localStorage` and passes it to
   `GramElementsProvider` via the `environment` config field. Gram forwards
   this value to the MCP server as a header.

## Passing credentials to MCP

The `environment` field on `ElementsConfig` maps to environment variables on
the MCP server. When you set:

```ts
environment: { MY_MCP_BEARER_TOKEN: token }
```

Gram delivers `token` to the MCP server as a header. The environment variable
name (`MY_MCP_BEARER_TOKEN` in this example) must match the name configured on
the MCP server's configuration page in the [Gram
dashboard](https://app.getgram.ai). You can find and configure these names
under your MCP server's settings.

## Setup

### 1. Install dependencies

```bash
npm install
```

### 2. Configure environment variables

Copy the `.env` file and fill in your values:

```bash
# Client-side (VITE_ prefix — bundled into the browser, so only non-secret values)
VITE_GRAM_PROJECT_SLUG=your-project-slug
VITE_GRAM_MCP_URL=https://app.getgram.ai/mcp/your-mcp-slug

# Server-side only (never sent to the browser)
GRAM_API_KEY=your-gram-api-key
```

- `VITE_GRAM_PROJECT_SLUG` — Your Gram project slug (visible in the dashboard
  URL)
- `VITE_GRAM_MCP_URL` — The MCP server URL from your Gram dashboard
- `GRAM_API_KEY` — Your Gram API key (keep this secret — it's only used
  server-side in `/api/chat/session`)

### 3. Run the dev server

```bash
npm run dev
```

Visit `http://localhost:3000`, sign in, and the chat UI will load on `/chat`.

## Key files

| File | Purpose |
|---|---|
| `src/routes/index.tsx` | Login page — authenticates the user and stores a token in `localStorage` |
| `src/routes/api/login.ts` | Login API endpoint — in a real app, replace this with your actual auth provider |
| `src/routes/chat.tsx` | Chat page — reads the token and passes it to `GramElementsProvider` as an MCP environment variable |
| `src/routes/api/chat.session.ts` | Session proxy — server-side route that calls Gram's session API using your secret API key |
| `.env` | Environment variable configuration |
