# OpenAI Apps SDK Example

This is an example project demonstrating how to use the OpenAI Apps SDK.



## Setup

From the project root, run:

```bash
pnpm install
```

then build the project:

```bash
pnpm build
```

Next, `cd` into the `pizzaz_node_server/pizza-app-gram` directory and run the following:

```bash
pnpm i @gram-ai/functions
pnpm run inline:app
pnpm build
gram auth
pnpm push
```

For more details refer to the gram [documentation](https://www.speakeasy.com/docs/gram/examples/open-ai-apps-sdk).
