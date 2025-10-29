---
"function-runners": patch
---

Bind handleToolCall to its object if needed in TypeScript runner entrypoint.

When handleToolCall is exported by an object, ensure it is bound to that object
so that any references to `this` inside the function work correctly. This was
breaking the Gram TS SDK which does this:

```
const gram = new Gram()
  .tool(/* ... */);

// We were calling gram.handleToolCall without binding it to `gram` in
// gram-start.mjs
export default gram;
```
