import type { Plugin } from "vite";

/**
 * Resolves a developer's gitignored `src/dev/slot.local.tsx` to an empty
 * module, so builds that must not reach a local slot cannot.
 *
 * The slot is a dev-server affordance: the brand row that renders it is
 * compiled out of production builds and disabled under test. But the eager
 * `import.meta.glob` that finds the slot is a build-time construct, so the
 * module is pulled in regardless of the runtime guard — and a local slot is
 * free to do whatever it likes at module scope, so its top-level statements
 * outlive the tree-shake that drops the unused binding. Omitting the module
 * outright keeps one developer's file out of their own production build, and
 * out of everyone's test run, without constraining what they may write in it.
 */
export function withoutLocalDevSlot(): Plugin {
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
