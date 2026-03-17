import { Outlet } from "react-router";
import { TopHeader } from "./top-header";
import { SidebarProvider } from "./ui/sidebar";

/**
 * Full-width layout for marketplace pages (no sidebar).
 * Used for the Templates marketplace which is a standalone browsing experience
 * rather than a project-scoped tool.
 *
 * Wraps in SidebarProvider because Page.Header renders a SidebarTrigger
 * which requires the sidebar context even when no sidebar is present.
 */
export const MarketplaceLayout = () => {
  return (
    <SidebarProvider>
      <div className="flex flex-col h-screen w-full">
        <TopHeader />
        <div className="flex-1 w-full overflow-hidden">
          <Outlet />
        </div>
      </div>
    </SidebarProvider>
  );
};
