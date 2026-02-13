# Gram Elements — Hono Example

Embeds [Gram Elements](https://docs.getgram.ai) in a Vite React app with a Hono backend using the `@gram-ai/elements/server/hono` adapter.

## Setup

```bash
pnpm install
cp .env.example .env   # fill in your values
pnpm dev
```

Visit `http://localhost:3000`, sign in, and the chat UI loads on `/chat`.

## Key files

| File                      | Purpose                                                   |
| ------------------------- | --------------------------------------------------------- |
| `server.ts`               | Hono server with `createHonoHandler` for session creation |
| `src/pages/ChatPage.tsx`  | Chat page using `GramElementsProvider` and `Chat`         |
| `src/pages/LoginPage.tsx` | Stub login form                                           |
