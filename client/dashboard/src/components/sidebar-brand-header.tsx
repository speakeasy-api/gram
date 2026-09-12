import type * as React from "react";

import { Link } from "react-router";

import { HatchRule } from "./hatch-rule";
import { GramLogo } from "./gram-logo";
import { DevWorktreeReadout } from "./dev-worktree-readout";
import { SidebarHeader, SidebarTrigger } from "@/components/ui/Sidebar";

// In development the brand row shows the worktree readout instead of the logo.
// A developer can replace it by dropping a gitignored src/dev-slot.local.tsx
// that exports a `DevSlot` component — the glob pattern must be a literal, and
// it resolves to an empty object when the file is absent, which is every stock
// checkout and every CI build, so nothing is imported and nothing is bundled.
const devSlots = import.meta.glob<{ DevSlot: () => React.ReactNode }>(
  "../dev-slot.local.tsx",
  { eager: true },
);

// Production builds and tests always render the stock logo. The readout and any
// local slot are dev-server affordances, so they must not reach a production
// artifact and must not change what the suite asserts — and a local slot's
// build-time constants come from that developer's vite.config.local.ts, which
// neither a production build nor vitest loads.
const isTest = import.meta.env.MODE === "test";
const isDevBrandRow = import.meta.env.DEV && !isTest;
const LocalDevSlot = isDevBrandRow
  ? Object.values(devSlots)[0]?.DevSlot
  : undefined;

/**
 * The brand row shared by the project and org sidebars: logo plus the collapse
 * control on one --header-height row, closed by the crosshatch rule so the
 * divider lines up with the page header's.
 *
 * In development the logo gives way to the worktree readout, or to a local dev
 * slot when one exists. Production always renders the logo.
 */
export function SidebarBrandHeader({
  homeHref,
}: {
  homeHref: string;
}): JSX.Element {
  return (
    <SidebarHeader className="gap-0 p-0">
      <div className="flex h-(--header-height) items-center justify-between gap-2 px-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
        <BrandSlot homeHref={homeHref} />
        {/* Collapse control sits beside the logo (WorkOS placement); search
            moved out to the page header. */}
        <SidebarTrigger />
      </div>
      <HatchRule />
    </SidebarHeader>
  );
}

function BrandSlot({ homeHref }: { homeHref: string }) {
  if (LocalDevSlot) return <LocalDevSlot />;
  if (isDevBrandRow) return <DevWorktreeReadout />;
  return (
    <Link
      to={homeHref}
      className="flex h-full items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
    >
      <GramLogo className="w-28" />
    </Link>
  );
}
