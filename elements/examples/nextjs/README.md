# Gram Elements — Next.js Example

Embeds [Gram Elements](https://docs.getgram.ai) in a Next.js App Router application using the `@gram-ai/elements/server/nextjs` adapter.

## Setup

```bash
pnpm install
cp .env.example .env   # fill in your values
pnpm dev
```

Visit `http://localhost:3000`, sign in, and the chat UI loads on `/chat`.

## Key files

| File | Purpose |
|---|---|
| `app/api/chat/session/route.ts` | Next.js route handler with `createNextHandler` for session creation |
| `app/chat/page.tsx` | Chat page using `GramElementsProvider` and `Chat` |
| `app/page.tsx` | Stub login form |
