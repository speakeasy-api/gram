import {
  useIsPlatformAdmin,
  useOrganization,
  useSession,
} from "@/contexts/Auth.tsx";
import { useSdkClient } from "@/contexts/Sdk.tsx";
import { cn } from "@/lib/utils";
import { DEMO_ORG_SLUG, PRE_DEMO_ORG_KEY } from "@/lib/demo";
import { useRBAC } from "@/hooks/useRBAC";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { Icon } from "@/components/ui/Icon";
import { ShieldAlert } from "lucide-react";
import { useCallback, useMemo } from "react";
import { Navigate, Outlet, useLocation } from "react-router";
import { AppSidebar } from "./app-sidebar.tsx";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { ChatLaunchOverlay } from "./chat-launch-overlay.tsx";
import { InsightsProvider } from "./insights-dock.tsx";
import { OrgSidebar } from "./org-sidebar.tsx";
import {
  SidePanelProvider,
  SidePanelSurface,
} from "./side-panel/SidePanel.tsx";
import { SidebarInset, SidebarProvider } from "@/components/ui/Sidebar";

// Layout to handle unauthenticated landing pages and the authenticated webapp experience
export const LoginCheck = (): JSX.Element => {
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

export const AppLayout = (): JSX.Element => {
  const isImpersonating = useShowsImpersonationBanner();

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "16rem",
          "--header-offset": isImpersonating ? "2.25rem" : "0px",
          ...(isImpersonating ? { "--banner-offset": "2.25rem" } : undefined),
        } as React.CSSProperties
      }
    >
      <SidePanelProvider>
        <AppLayoutContent isImpersonating={isImpersonating} />
      </SidePanelProvider>
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

// The shared demo org isn't a customer org being impersonated — brand it as
// a demo instead of an impersonation warning. It is entered either through
// the admin override cookie or session-side via auth.enterDemo (any user),
// so demo detection keys off the active org slug, not the cookie.

/** Banner shows for admin cookie-impersonation or any session in the demo org. */
const useShowsImpersonationBanner = (): boolean => {
  const isAdmin = useIsPlatformAdmin();
  const organization = useOrganization();
  const overrideSlug = useMemo(() => getAdminOverrideCookie(), []);
  return (isAdmin && !!overrideSlug) || organization.slug === DEMO_ORG_SLUG;
};

const ImpersonationBanner = () => {
  const organization = useOrganization();
  const session = useSession();
  const client = useSdkClient();
  const isDemo = organization.slug === DEMO_ORG_SLUG;

  const exit = () => {
    void (async () => {
      document.cookie = "gram_admin_override=; path=/; max-age=0;";
      // Exiting the demo switches back to the org the user came from (stashed
      // by /explore-demo), or their first real org — no logout round-trip.
      // Falls through to logout otherwise (e.g. admin cookie-impersonation,
      // or a user with no other org).
      const preDemoOrgId = localStorage.getItem(PRE_DEMO_ORG_KEY);
      const ownOrg =
        session.organizations.find(
          (org) => org.id === preDemoOrgId && org.slug !== DEMO_ORG_SLUG,
        ) ?? session.organizations.find((org) => org.slug !== DEMO_ORG_SLUG);
      if (isDemo && ownOrg) {
        localStorage.removeItem(PRE_DEMO_ORG_KEY);
        await client.auth.switchScopes({ organizationId: ownOrg.id });
        window.location.replace("/");
        return;
      }
      await client.auth.logout();
      window.location.href = "/login";
    })();
  };

  // Height must stay 2.25rem (h-9) to match --header-offset / --banner-offset.
  // Solid ink-family bars (editorial): demo = ink, impersonation = deep brand
  // red. White mono label; the exit action is a hairline-outlined light chip.
  const toneClasses = isDemo
    ? "bg-surface-primary-fixed-dark border-transparent"
    : "bg-destructive-highlight border-transparent";
  const labelTone = "text-default-fixed-light";

  return (
    <div
      className={cn(
        "flex h-9 items-center justify-center gap-3 border-b px-4",
        toneClasses,
      )}
    >
      <ShieldAlert className={cn("h-3.5 w-3.5 shrink-0", labelTone)} />
      {/* Plain concatenation: tailwind-merge would treat text-eyebrow and the
          text-default-* tone as conflicting text-* utilities and drop one. */}
      <span className={`text-eyebrow ${labelTone}`}>
        {isDemo
          ? "Demo org — sample data"
          : `Impersonating ${organization.slug}`}
      </span>
      <button
        type="button"
        onClick={exit}
        className="border-neutral-softest text-default-fixed-light ml-2 border px-2 py-0.5 font-mono text-[11px] tracking-[0.08em] uppercase hover:bg-white/10"
      >
        {isDemo ? "Exit demo" : "Stop impersonating"}
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
      <div className="flex w-full flex-1 overflow-hidden">
        <AppSidebar variant="inset" />
        <SidebarInset>
          <GlobalInsightsWrapper>
            <MembershipSyncGuard>
              <Outlet />
            </MembershipSyncGuard>
          </GlobalInsightsWrapper>
        </SidebarInset>
        {/* Sibling of the content, not an overlay: the page reflows into the
            remaining width so nothing sits behind the panel. */}
        <SidePanelSurface />
      </div>
      {/* Above the outlet so the suggestion → chat bubble morph survives the
          navigation into the chat route. */}
      <ChatLaunchOverlay />
    </div>
  );
};

/**
 * Wraps every project-scoped page in a single InsightsProvider so the
 * docked Project Assistant composer floats at the bottom of the content
 * area across the whole project app. Pages mount <InsightsConfig /> to
 * override the defaults (custom prompt/suggestions/MCP filter).
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
      title="How can I help you understand your AI usage?"
      subtitle="Your assistant for exploring the platform — logs, traces, MCP servers, and more."
      suggestions={INSIGHTS_SUGGESTIONS.default}
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
          className="bg-primary text-primary-foreground hover:bg-primary/90 mt-2 px-4 py-2 text-sm font-medium"
          onClick={() => {
            void (async () => {
              await client.auth.logout();
              window.location.href = "/login";
            })();
          }}
        >
          Log out
        </button>
      </div>
    </div>
  );
};

export const OrgLayout = (): JSX.Element => {
  const isImpersonating = useShowsImpersonationBanner();

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "16rem",
          "--header-offset": isImpersonating ? "2.25rem" : "0px",
          ...(isImpersonating ? { "--banner-offset": "2.25rem" } : undefined),
        } as React.CSSProperties
      }
    >
      <div className="flex h-screen w-full flex-col">
        {isImpersonating && <ImpersonationBanner />}
        <div className="flex w-full flex-1 overflow-hidden">
          <OrgSidebar variant="inset" />
          <SidebarInset>
            <MembershipSyncGuard>
              <Outlet />
            </MembershipSyncGuard>
          </SidebarInset>
        </div>
      </div>
    </SidebarProvider>
  );
};
