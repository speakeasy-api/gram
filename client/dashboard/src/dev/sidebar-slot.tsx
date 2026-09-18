import type * as React from "react";

import { DevWorktreeReadout } from "./worktree-readout";

// A developer can replace the readout by dropping a gitignored
// src/dev/slot.local.tsx that exports a `DevSlot` component — the glob pattern
// must be a literal, and it resolves to an empty object when the file is
// absent, which is every stock checkout and every CI build, so nothing is
// imported and nothing is bundled.
const localSlots = import.meta.glob<{ DevSlot: () => React.ReactNode }>(
  "./slot.local.tsx",
  { eager: true },
);

// Production builds and tests get the stock logo instead. The readout and any
// local slot are dev-server affordances, so they must not reach a production
// artifact and must not change what the suite asserts — and a local slot's
// build-time constants come from that developer's vite.config.local.ts, which
// neither a production build nor vitest loads. Both conditions fold to a
// constant at build time, so the branch not taken is dropped from the bundle
// along with everything it reaches.
const isTest = import.meta.env.MODE === "test";
const enabled = import.meta.env.DEV && !isTest;

/**
 * What the sidebar footer shows above the user menu during development: a
 * developer's local slot when they have one, otherwise the stock worktree
 * readout. `undefined` in production builds and under vitest, where the
 * footer renders nothing extra.
 */
export const DevSidebarSlot: React.ComponentType | undefined = enabled
  ? (Object.values(localSlots)[0]?.DevSlot ?? DevWorktreeReadout)
  : undefined;
