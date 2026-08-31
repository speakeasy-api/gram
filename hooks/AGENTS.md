# Hooks runtime

- Keep provider wire decoding and response encoding in `agenthooks`; relay code consumes canonical events.
- Treat `copilot-cli` and `vscode-copilot` as distinct providers. They can share one registration but never a codec.
- Preserve fail-open transport behavior, the credential ratchet, idempotency, local redaction, and spool bounds.
- Add a relay regression test for every newly enabled provider event or decision path.
- Never log credentials or copy generated credentials into fixtures.
