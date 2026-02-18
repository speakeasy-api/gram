# Gram Elements — Fastify Example

Embeds [Gram Elements](https://docs.getgram.ai) in a Vite React app with a Fastify backend using the `@gram-ai/elements/server/fastify` adapter.

## Setup

```bash
pnpm install
cp .env.example .env   # fill in your values
pnpm dev
```

Visit `http://localhost:3000`, sign in, and the chat UI loads on `/chat`.

## Key files

| File                      | Purpose                                                         |
| ------------------------- | --------------------------------------------------------------- |
| `server.ts`               | Fastify server with `createFastifyHandler` for session creation |
| `src/pages/ChatPage.tsx`  | Chat page using `GramElementsProvider` and `Chat`               |
| `src/pages/LoginPage.tsx` | Stub login form                                                 |
