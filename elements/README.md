# `@gram-ai/elements`

Elements is a library built for the agentic age. We provide customizable and elegant UI primitives for building chat-like experiences for MCP Servers. Works best with Gram MCP, but also supports connecting any MCP Server.

## Setup

First ensure that you have installed the required peer dependencies:

```bash
pnpm add react react-dom
```

## Usage

`@gram-ai/elements` requires that you wrap your React tree with our context provider and reference our CSS:

```jsx
import { GramElementsProvider } from "@gram-ai/elements";
import "@gram-ai/elements/elements.css";

export const App = () => {
  return (
    <GramElementsProvider>
      <h1>Hello world!</h1>
    </GramElementsProvider>
  );
};
```

## Contributing

We welcome pull requests to Elements. Please read the contributing guide.
