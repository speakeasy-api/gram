import { useSession } from "@/contexts/Auth.tsx";
import { Navigate, Outlet, useLocation } from "react-router";
import { AppSidebar } from "./app-sidebar.tsx";
import { SidebarInset, SidebarProvider } from "./ui/sidebar.tsx";
import { Modal, ModalProvider, useModal } from "@speakeasy-api/moonshine";
import { useEffect } from "react";
import { useLocalStorageState } from "@/hooks/useLocalStorageState.ts";
import { FunctionsAnnouncementModal } from "./functions-announcement-modal/index.tsx";

// Layout to handle unauthenticated landing pages and the authenticated webapp experience
export const LoginCheck = () => {
  const session = useSession();
  const location = useLocation();

  if (session.session === "") {
    const redirectTo = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?redirect=${redirectTo}`} />;
  }

  if (!session.activeOrganizationId) {
    const redirectTo = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/register?redirect=${redirectTo}`} />;
  }

  return <Outlet />;
};

export const AppLayout = () => {
  return (
    <SidebarProvider>
      <ModalProvider>
        <AppLayoutContent />
      </ModalProvider>
    </SidebarProvider>
  );
};

const AppLayoutContent = () => {
  const [hasSeenFunctionsModal, setHasSeenFunctionsModal] =
    useLocalStorageState(
      "gram-dashboard-has-seen-functions-announcement-modal",
      false,
    );
  const { openScreen } = useModal();
  const handleModalClose = () => {
    setHasSeenFunctionsModal(true);
  };
  // if they have not seen the feature request modal, show it
  useEffect(() => {
    if (!hasSeenFunctionsModal) {
      openScreen({
        title: "Gram Functions Announcement",
        component: (
          <FunctionsAnnouncementModal
            onClose={() => setHasSeenFunctionsModal(true)}
          />
        ),
        id: "new-feature",
      });
    }
  }, [openScreen, hasSeenFunctionsModal, handleModalClose]);

  return (
    <>
      <AppSidebar variant="inset" />
      <SidebarInset>
        <Outlet />
        <Modal
          closable
          className="rounded-sm min-w-auto p-0 h-full 2xl:w-2/3 w-9/12 max-w-[1100px] 2xl:max-w-[1000px] max-h-[450px] min-h-auto"
          layout="custom"
          onClose={handleModalClose}
        />
      </SidebarInset>
    </>
  );
};
