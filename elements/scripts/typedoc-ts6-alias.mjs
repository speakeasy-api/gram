// TypeDoc drives the TypeScript JS compiler API, which typescript@7 (tsgo) no
// longer ships, and pnpm resolves TypeDoc's `typescript` peer to this package's
// typescript@7. pnpm's `overrides` don't apply to peer dependencies, so instead
// this hook redirects `import "typescript"` inside the TypeDoc process to the
// JS-based TS6 compat compiler. Drop it once TypeDoc supports TS7.
import { registerHooks } from "node:module";

const ts6 = import.meta.resolve("@typescript/typescript6");

registerHooks({
  resolve(specifier, context, nextResolve) {
    if (specifier === "typescript") {
      return { url: ts6, shortCircuit: true };
    }
    return nextResolve(specifier, context);
  },
});
