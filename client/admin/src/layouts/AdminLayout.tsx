import { useState, type CSSProperties, type JSX } from "react";
import { Outlet } from "@tanstack/react-router";

import { AppSidebar } from "@/components/app-sidebar";
import { SiteHeader } from "@/components/site-header";
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar";

// Stock shadcn writes this cookie on every toggle but leaves the read to the
// application. The value only decides the first mount, so read it once, and
// read it on mount rather than on import so no browser global runs at module
// scope.
function sidebarOpen(): boolean {
  return !document.cookie.split("; ").includes("sidebar_state=false");
}

export function AdminLayout(): JSX.Element {
  const [defaultOpen] = useState(sidebarOpen);

  return (
    <SidebarProvider
      defaultOpen={defaultOpen}
      style={
        {
          "--sidebar-width": "calc(var(--spacing) * 72)",
          "--header-height": "calc(var(--spacing) * 12)",
        } as CSSProperties
      }
    >
      <AppSidebar />

      <SidebarInset>
        <SiteHeader />
        {/* The chain of min-h-0 is what lets a page ask for the height that is
            left. One link without it makes every child below content-sized, so
            a page that wants to fill has to guess a vh number instead. The
            scroll sits on the innermost one, so a page taller than the shell
            scrolls under the header rather than being cut off. */}
        <div className="flex min-h-0 flex-1 flex-col">
          <div className="@container/main flex min-h-0 flex-1 flex-col gap-2">
            <div className="flex min-h-0 flex-1 flex-col gap-4 py-4 md:gap-6 md:py-6">
              <div className="flex min-h-0 flex-1 flex-col overflow-auto px-4 lg:px-6">
                <Outlet />
              </div>
            </div>
          </div>
        </div>
      </SidebarInset>
    </SidebarProvider>
  );
}
