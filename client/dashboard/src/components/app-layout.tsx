import { useSession } from "@/contexts/Auth.tsx";
import { Navigate, Outlet, useLocation } from "react-router";
import { AppSidebar } from "./app-sidebar.tsx";
import { TooltipProvider } from "./ui/tooltip.tsx";
import { SidebarInset, SidebarProvider } from "./ui/sidebar.tsx";

// Layout to handle unauthenticated landing pages and the authenticated webapp experience
export const LoginCheck = () => {
  const session = useSession();
  const location = useLocation();

  if (session.session === "") {
    return <Navigate to={`/login${location.search}`} />;
  }

  if (!session.activeOrganizationId) {
    return <Navigate to={`/register${location.search}`} />;
  }

  return <Outlet />;
};

export const AppLayout = () => {
  return (
    <SidebarProvider>
      <TooltipProvider delayDuration={0}>
        <AppSidebar variant="inset" />
      </TooltipProvider>
      <SidebarInset>
        <TooltipProvider>
          <Outlet />
        </TooltipProvider>
      </SidebarInset>
    </SidebarProvider>
  );
};
