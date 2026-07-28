# opencode → Speakeasy Gram: MVP demo

One opencode plugin that POSTs a tool event to Gram's unified `hooks.ingest`
endpoint. No backend/frontend changes.

## 1. Get a hooks-scoped key + project slug

In the local dashboard, open the **hooks / observability setup** (the flow that
generates the Claude/Cursor/Codex observability plugin). It shows a `Gram-Key`
and `Gram-Project`. Copy both — that key already has the required `hooks` scope.

## 2. Install the plugin

opencode auto-loads plugins from `.opencode/plugins/` (project) or
`~/.config/opencode/plugins/` (global). Global works from anywhere:

```sh
mkdir -p ~/.config/opencode/plugins
cp gram.ts ~/.config/opencode/plugins/gram.ts
```

> If it doesn't load, your opencode build may use the singular `plugin/` dir —
> try `~/.config/opencode/plugin/gram.ts`.

## 3. Run opencode with the env set

```sh
NODE_TLS_REJECT_UNAUTHORIZED=0 \
GRAM_URL=https://localhost:8080 \
GRAM_KEY=<paste hooks key> \
GRAM_PROJECT=<paste project slug> \
opencode
```

(`NODE_TLS_REJECT_UNAUTHORIZED=0` is only for the local self-signed cert. Against
a real deployment, drop it and set `GRAM_URL` to that host.)

## 4. Trigger a tool

In the opencode session, ask it to do anything that runs a tool — e.g. "read
README.md" or "run `ls`". Each tool call prints `[gram] tool.completed -> 200`
in the opencode logs.

## 5. See it in Gram

The event lands as an `opencode` source in the dashboard's hooks/observability
views (and the session/chat view for that project). That's the demo: an opencode
tool event registered to Speakeasy.

## Notes / next steps (not in MVP)

- Wired: `session.created` → `session.started` and `tool.execute.after` →
  `tool.completed`. Add prompts and assistant messages for a fuller picture.
- Distribution would become an npm package (`opencode.json` `"plugin": [...]`)
  so users add one line instead of copying a file.
- Optional backend polish: a `parseOpencodeHookEvent` + `case "opencode"` in
  `ingest_hooks.go` so the telemetry event name carries opencode-native names
  (canonical fallback already works without it).
