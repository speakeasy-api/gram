import path from "node:path";
import { defineConfig } from "vitest/config";
import type { Plugin } from "vite";
import react from "@vitejs/plugin-react";

// The dev brand row is disabled under test, but the eager glob that looks for a
// developer's gitignored src/dev/slot.local.tsx is a build-time construct and
// resolves it anyway — which would drag that file, and whatever it imports,
// into every test that renders a sidebar. Resolve it to nothing here so the
// suite renders the stock logo on its own terms rather than by a runtime guard,
// and so one developer's local file cannot break it for everyone.
function withoutLocalDevSlot(): Plugin {
  const absent = "\0gram:absent-dev-slot";

  return {
    name: "gram-without-local-dev-slot",
    enforce: "pre",
    resolveId(id) {
      return id === "./slot.local.tsx" || id.endsWith("/slot.local.tsx")
        ? absent
        : null;
    },
    load(id) {
      return id === absent ? "export {}" : null;
    },
  };
}

export default defineConfig({
  plugins: [withoutLocalDevSlot(), react()],
  resolve: {
    alias: {
      "@": path.resolve(import.meta.dirname, "./src"),
      "@gram/client": path.resolve(import.meta.dirname, "./src/sdk/src"),
    },
    conditions: ["source"],
  },
  define: {
    __GRAM_SERVER_URL__: JSON.stringify(""),
    __GRAM_GIT_SHA__: JSON.stringify(""),
    __GRAM_API_URL__: JSON.stringify(""),
    __GRAM_DEV_WORKTREE__: JSON.stringify(""),
    __GRAM_DEV_BRANCH__: JSON.stringify(""),
    __GRAM_DEV_BRANCH_EVENT__: JSON.stringify(""),
    __GRAM_DEV_BRANCH_ASK__: JSON.stringify(""),
  },
  test: {
    environment: "happy-dom",
  },
});
