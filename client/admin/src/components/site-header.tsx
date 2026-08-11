import type { JSX } from "react";
import { useLocation } from "react-router";

import { Separator } from "@/components/ui/separator";
import { SidebarTrigger } from "@/components/ui/sidebar";

function pageTitle(pathname: string): string {
  if (pathname.startsWith("/projects")) return "Projects";
  return "Organizations";
}

export function SiteHeader(): JSX.Element {
  const { pathname } = useLocation();

  return (
    <header className="flex h-(--header-height) shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear group-has-data-[collapsible=icon]/sidebar-wrapper:h-(--header-height)">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator
          orientation="vertical"
          className="mx-2 data-[orientation=vertical]:h-4"
        />
        <h1 className="text-base font-medium">{pageTitle(pathname)}</h1>
      </div>
    </header>
  );
}
