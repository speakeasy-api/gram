import { Navigate, Outlet } from "react-router-dom";
import { SidebarInset } from "./ui/sidebar.tsx";
import { AppSidebar } from "./app-sidebar.tsx";
import { SidebarProvider } from "./ui/sidebar.tsx";
import { useSession } from "@/contexts/Auth.tsx";

// Layout to handle unauthenticated landing pages and the authenticated webapp experience
export const AppLayout = () => {
  const session = useSession();
  if (session.session === "") {
    return <Navigate to="/login" />
  }
  
  return (
    <SidebarProvider>
      <AppSidebar variant="inset" />
      <SidebarInset>
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  );
};

