import { useIsAdmin, useOrganization, useSession } from "@/contexts/Auth.tsx";
import { useSdkClient } from "@/contexts/Sdk.tsx";
import { useRBAC } from "@/hooks/useRBAC";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { Icon, Modal, ModalProvider } from "@speakeasy-api/moonshine";
import { ShieldAlert } from "lucide-react";
import { useCallback, useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router";
import { AppSidebar } from "./app-sidebar.tsx";
import { BrandGradientLine } from "./brand-gradient-line.tsx";
import { InsightsProvider } from "./insights-sidebar.tsx";
import { OrgSidebar } from "./org-sidebar.tsx";
import { TopHeader } from "./top-header.tsx";
import { SidebarInset, SidebarProvider } from "./ui/sidebar.tsx";

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
  const isAdmin = useIsAdmin();
  const overrideSlug = useMemo(() => getAdminOverrideCookie(), []);
  const isImpersonating = isAdmin && !!overrideSlug;

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "14rem",
          ...(isImpersonating
            ? {
                "--header-offset": "5.75rem",
                "--banner-offset": "2.25rem",
              }
            : undefined),
        } as React.CSSProperties
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
    <div className="flex items-center justify-center gap-3 bg-red-600 px-4 py-2 text-sm text-white">
      <ShieldAlert className="h-4 w-4 shrink-0" />
      <span className="font-mono font-bold">
        Impersonating {organization.slug}
      </span>
      <button
        type="button"
        className="ml-2 rounded bg-white/20 px-2 py-0.5 text-xs font-medium transition-colors hover:bg-white/30"
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
    <div className="flex h-screen w-full flex-col">
      {isImpersonating && <ImpersonationBanner />}
      <TopHeader />
      <BrandGradientLine />
      <div className="flex w-full flex-1 overflow-hidden pt-2">
        <AppSidebar variant="inset" />
        <SidebarInset>
          <GlobalInsightsWrapper>
            <MembershipSyncGuard>
              <Outlet />
            </MembershipSyncGuard>
            <Modal
              closable
              className="h-full max-h-[450px] min-h-auto w-9/12 max-w-[1100px] min-w-auto rounded-sm p-0 2xl:w-2/3 2xl:max-w-[1000px]"
              layout="custom"
            />
          </GlobalInsightsWrapper>
        </SidebarInset>
      </div>
    </div>
  );
};

/**
 * Wraps every project-scoped page in a single InsightsProvider so the
 * AI Insights trigger lives statically in the top breadcrumb bar across
 * the whole project app. Pages mount <InsightsConfig /> to override the
 * defaults (custom prompt/suggestions/MCP filter).
 */
const GlobalInsightsWrapper = ({ children }: { children: React.ReactNode }) => {
  // Default config: include all observability tools (no filter), so the
  // global assistant can answer about anything. Pages narrow this via
  // <InsightsConfig mcpConfig={...} /> when they want a focused tool set.
  const includeAll = useCallback(() => true, []);
  const mcpConfig = useObservabilityMcpConfig({ toolsToInclude: includeAll });

  return (
    <InsightsProvider
      mcpConfig={mcpConfig}
      title="Ask AI"
      subtitle="Your assistant for exploring Gram — logs, traces, MCP servers, and more."
      suggestions={[
        {
          title: "Summarize errors",
          label: "Recent error trends",
          prompt:
            "Summarize the most common error patterns in the last 24 hours.",
        },
        {
          title: "Top tool calls",
          label: "Most-called tools",
          prompt: "Which tools have been called most often this week?",
        },
        {
          title: "Slow tools",
          label: "Latency outliers",
          prompt: "Find tools with the slowest p95 latency in the last day.",
        },
      ]}
    >
      {children}
    </InsightsProvider>
  );
};

/**
 * Guards against a failed grants query (e.g. the user's org membership hasn't
 * synced yet). Shows a recovery prompt instead of crashing the app.
 */
const MembershipSyncGuard = ({ children }: { children: React.ReactNode }) => {
  const { error } = useRBAC();
  const client = useSdkClient();

  if (!error) return <>{children}</>;

  return (
    <div className="flex h-full min-h-[400px] w-full items-center justify-center">
      <div className="flex max-w-md flex-col items-center gap-4 text-center">
        <div className="bg-muted flex h-12 w-12 items-center justify-center rounded-full">
          <Icon name="refresh-cw" className="text-muted-foreground h-5 w-5" />
        </div>
        <h2 className="text-lg font-medium">Organization sync required</h2>
        <p className="text-muted-foreground text-sm">
          Your organization membership needs to be re-synchronized. Please log
          out and log back in to refresh your session.
        </p>
        <button
          type="button"
          className="bg-primary text-primary-foreground hover:bg-primary/90 mt-2 rounded-md px-4 py-2 text-sm font-medium"
          onClick={async () => {
            await client.auth.logout();
            window.location.href = "/login";
          }}
        >
          Log out
        </button>
      </div>
    </div>
  );
};

export const OrgLayout = () => {
  const isAdmin = useIsAdmin();
  const overrideSlug = useMemo(() => getAdminOverrideCookie(), []);
  const isImpersonating = isAdmin && !!overrideSlug;

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "14rem",
          ...(isImpersonating
            ? {
                "--header-offset": "5.75rem",
                "--banner-offset": "2.25rem",
              }
            : undefined),
        } as React.CSSProperties
      }
    >
      <ModalProvider>
        <div className="flex h-screen w-full flex-col">
          {isImpersonating && <ImpersonationBanner />}
          <TopHeader />
          <BrandGradientLine />
          <div className="flex w-full flex-1 overflow-hidden pt-2">
            <OrgSidebar variant="inset" />
            <SidebarInset>
              <MembershipSyncGuard>
                <Outlet />
              </MembershipSyncGuard>
            </SidebarInset>
          </div>
        </div>
      </ModalProvider>
    </SidebarProvider>
  );
};
