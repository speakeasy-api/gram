# Gram Elements — TanStack Start Example

Embeds [Gram Elements](https://docs.getgram.ai) in a TanStack Start application with two session creation approaches:

- **API Route** (`/chat`) — traditional endpoint at `/api/chat/session` using `createTanStackStartHandler`
- **Server Function** (`/chat/server-fn`) — RPC-style `createServerFn` passed directly to `sessionFn`

## Setup

```bash
pnpm install
cp .env.example .env   # fill in your values
pnpm dev
```

Visit `http://localhost:3000`, sign in, and choose a chat approach.

## Key files

| File                             | Purpose                                                                            |
| -------------------------------- | ---------------------------------------------------------------------------------- |
| `src/routes/api/chat.session.ts` | Server route with `createTanStackStartHandler` for session creation                |
| `src/session.functions.ts`       | Server function with `createTanStackStartSessionFn` for RPC-style session creation |
| `src/routes/chat.tsx`            | Chat page using the API route approach                                             |
| `src/routes/chat_.server-fn.tsx` | Chat page using the server function approach                                       |
| `src/routes/index.tsx`           | Stub login form                                                                    |
