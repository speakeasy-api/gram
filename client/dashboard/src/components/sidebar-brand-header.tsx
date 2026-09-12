import { Link } from "react-router";

import { HatchRule } from "./hatch-rule";
import { GramLogo } from "./gram-logo";
import { DevBrandSlot } from "@/dev/brand-slot";
import { SidebarHeader, SidebarTrigger } from "@/components/ui/Sidebar";

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
  if (DevBrandSlot) return <DevBrandSlot />;
  return (
    <Link
      to={homeHref}
      className="flex h-full items-center px-1 hover:no-underline group-data-[collapsible=icon]:hidden"
    >
      <GramLogo className="w-28" />
    </Link>
  );
}
