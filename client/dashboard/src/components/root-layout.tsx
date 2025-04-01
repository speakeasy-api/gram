import { Outlet } from "react-router-dom";
import { SidebarInset } from "./ui/sidebar.tsx";
import { AppSidebar } from "./app-sidebar.tsx";
import { SidebarProvider } from "./ui/sidebar.tsx";

export const RootLayout = () => {
  return (
    <SidebarProvider>
      <AppSidebar variant="inset" />
      <SidebarInset>
        <Outlet />
      </SidebarInset>
    </SidebarProvider>
  );
};
