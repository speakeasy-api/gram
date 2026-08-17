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
    // The constraint belongs here rather than in sidebar.tsx: that file is
    // vendored shadcn and stays pristine so the registry can be re-pulled.
    <SidebarProvider
      className="h-svh"
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
        {/* One link without min-h-0 makes every child below content-sized. */}
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
