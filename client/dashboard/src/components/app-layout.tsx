import { useIsAdmin, useOrganization, useSession } from "@/contexts/Auth.tsx";
import { useSdkClient } from "@/contexts/Sdk.tsx";
import { Modal, ModalProvider } from "@speakeasy-api/moonshine";
import { ShieldAlert } from "lucide-react";
import { useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router";
import { AppSidebar } from "./app-sidebar.tsx";
import { TopHeader } from "./top-header.tsx";
import { SidebarInset, SidebarProvider } from "./ui/sidebar.tsx";

// Layout to handle unauthenticated landing pages and the authenticated webapp experience
export const LoginCheck = () => {
  const session = useSession();
  const location = useLocation();

  if (session.session === "" && !import.meta.env.DEV) {
    const redirectTo = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/login?redirect=${redirectTo}`} />;
  }

  if (!session.activeOrganizationId && !import.meta.env.DEV) {
    const redirectTo = encodeURIComponent(location.pathname + location.search);
    return <Navigate to={`/register?redirect=${redirectTo}`} />;
  }

  return <Outlet />;
};

export const AppLayout = () => {
  const isAdmin = useIsAdmin();
  const overrideSlug = useMemo(() => getAdminOverrideCookie(), []);
  const isImpersonating = isAdmin && !!overrideSlug;

  return (
    <SidebarProvider
      style={
        isImpersonating
          ? ({ "--header-offset": "5.75rem" } as React.CSSProperties)
          : undefined
      }
    >
      <ModalProvider>
        <AppLayoutContent isImpersonating={isImpersonating} />
      </ModalProvider>
    </SidebarProvider>
  );
};

function getAdminOverrideCookie(): string | null {
  const match = document.cookie
    .split("; ")
    .find((row) => row.startsWith("gram_admin_override="));
  if (!match) return null;
  const value = match.split("=")[1];
  return value || null;
}

const ImpersonationBanner = () => {
  const organization = useOrganization();
  const client = useSdkClient();

  return (
    <div className="flex items-center justify-center gap-3 bg-red-600 px-4 py-2 text-white text-sm">
      <ShieldAlert className="h-4 w-4 shrink-0" />
      <span className="font-bold font-mono">
        Impersonating {organization.slug}
      </span>
      <button
        type="button"
        className="ml-2 rounded bg-white/20 px-2 py-0.5 text-xs font-medium hover:bg-white/30 transition-colors"
        onClick={async () => {
          document.cookie = "gram_admin_override=; path=/; max-age=0;";
          await client.auth.logout();
          window.location.href = "/login";
        }}
      >
        Stop impersonating
      </button>
    </div>
  );
};

const AppLayoutContent = ({
  isImpersonating,
}: {
  isImpersonating: boolean;
}) => {
  return (
    <div className="flex flex-col h-screen w-full">
      {isImpersonating && <ImpersonationBanner />}
      <TopHeader />
      <div className="flex flex-1 w-full overflow-hidden pt-2">
        <AppSidebar variant="inset" />
        <SidebarInset>
          <Outlet />
          <Modal
            closable
            className="rounded-sm min-w-auto p-0 h-full 2xl:w-2/3 w-9/12 max-w-[1100px] 2xl:max-w-[1000px] max-h-[450px] min-h-auto"
            layout="custom"
          />
        </SidebarInset>
      </div>
    </div>
  );
};
