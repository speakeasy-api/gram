import type * as React from "react";

import { Link } from "react-router";

import { HatchRule } from "./hatch-rule";
import { GramLogo } from "./gram-logo";
import { SidebarHeader, SidebarTrigger } from "@/components/ui/Sidebar";

// Optional dev-only slot: drop a gitignored src/dev-slot.local.tsx exporting a
// `DevSlot` component to take over the brand row — handy for labelling which
// worktree's stack a tab is pointed at. The pattern must be a literal, and it
// resolves to an empty object when the file is absent, which is every stock
// checkout and every CI build, so nothing is imported and nothing is bundled.
const devSlots = import.meta.glob<{ DevSlot: () => React.ReactNode }>(
  "../dev-slot.local.tsx",
  { eager: true },
);
const DevSlot = Object.values(devSlots)[0]?.DevSlot;

/**
 * The brand row shared by the project and org sidebars: logo plus the collapse
 * control on one --header-height row, closed by the crosshatch rule so the
 * divider lines up with the page header's.
 *
 * The logo gives way to a local dev slot when one exists; with no slot, which
 * is every stock checkout and every production build, it renders as always.
 */
export function SidebarBrandHeader({
  homeHref,
}: {
  homeHref: string;
}): JSX.Element {
  return (
    <SidebarHeader className="gap-0 p-0">
      <div className="flex h-(--header-height) items-center justify-between gap-2 px-2 group-data-[collapsible=icon]:justify-center group-data-[collapsible=icon]:px-0">
        {DevSlot ? (
          <DevSlot />
        ) : (
          <Link
            to={homeHref}
            className="flex h-full items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
          >
            <GramLogo className="w-28" />
          </Link>
        )}
        {/* Collapse control sits beside the logo (WorkOS placement); search
            moved out to the page header. */}
        <SidebarTrigger />
      </div>
      <HatchRule />
    </SidebarHeader>
  );
}
