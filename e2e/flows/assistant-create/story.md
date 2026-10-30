# Create an assistant with the builder

A user describes the assistant they want in plain words, and the builder
creates it.

## Given

- The local stack has an LLM key (`OPENROUTER_DEV_KEY`), so the builder can
  answer.
- The user can write assistants.

## When

1. From the assistants list, start a new assistant.
2. Ask the builder: "Create an assistant named `Flow bot <runId>` that answers
   questions about our docs. Do not add any tools or triggers."
3. Wait for the builder to create the assistant.
4. Go back to the assistants list.

## Then

- The builder replies in the chat.
- The page URL gains the new assistant's `id`.
- `assistants.list` includes an assistant named `Flow bot <runId>`.
- The assistants list shows `Flow bot <runId>`.
