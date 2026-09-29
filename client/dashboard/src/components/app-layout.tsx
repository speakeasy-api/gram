import { useOrganization, useSession } from "@/contexts/Auth.tsx";
import { useSdkClient } from "@/contexts/Sdk.tsx";
import { cn } from "@/lib/utils";
import { DEMO_ORG_SLUG, PRE_DEMO_ORG_KEY } from "@/lib/demo";
import { logoutToLogin } from "@/lib/logout-to-login";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { useObservabilityMcpConfig } from "@/hooks/useObservabilityMcpConfig";
import { Icon } from "@/components/ui/Icon";
import { ShieldAlert, X } from "lucide-react";
import { useCallback, useEffect } from "react";
import { Navigate, Outlet, useLocation, useNavigate } from "react-router";
import { AppSidebar } from "./app-sidebar.tsx";
import { INSIGHTS_SUGGESTIONS } from "@/lib/insights-suggestions";
import { ChatLaunchOverlay } from "./chat-launch-overlay.tsx";
import { InsightsProvider } from "./insights-dock.tsx";
import { SettingsNav } from "./settings-nav.tsx";
import {
  SettingsOverlayContext,
  lastAppPath,
  rememberAppPath,
} from "./settings-overlay-context";
import { resolveLandingProject } from "@/lib/preferredProject";
import {
  SidePanelProvider,
  SidePanelSurface,
} from "./side-panel/SidePanel.tsx";
import { SidebarInset, SidebarProvider } from "@/components/ui/Sidebar";
import { useShowsImpersonationBanner } from "./impersonation-banner-state";
import { ModeSurface } from "./mode-switch-stage.tsx";
import { ModeSwitcher } from "./mode-switcher.tsx";

// Height of the impersonation banner above the app surface (h-9 / 2.25rem).
const chromeTopOffset = (isImpersonating: boolean): string =>
  isImpersonating ? "2.25rem" : "0px";

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
    return <Navigate to={`/sign-up?redirect=${redirectTo}`} />;
  }

  return <Outlet />;
};

export const AppLayout = (): JSX.Element => {
  const isImpersonating = useShowsImpersonationBanner();
  const chromeOffset = chromeTopOffset(isImpersonating);

  return (
    <SidebarProvider
      style={
        {
          "--sidebar-width": "16rem",
          // The mode switcher overlays the page header, so only the
          // impersonation banner offsets the fixed app surface.
          "--header-offset": chromeOffset,
          "--banner-offset": chromeOffset,
        } as React.CSSProperties
      }
    >
      <SidePanelProvider>
        <AppLayoutContent isImpersonating={isImpersonating} />
      </SidePanelProvider>
    </SidebarProvider>
  );
};

// The shared demo org isn't a customer org being impersonated — brand it as
// a demo instead of an impersonation warning. Detection keys off the active
// organization so it also works for sessions entered through auth.enterDemo.

export const ImpersonationBanner = (): JSX.Element => {
  const organization = useOrganization();
  const session = useSession();
  const client = useSdkClient();
  const isDemo = organization.slug === DEMO_ORG_SLUG;
  const isWorkOSImpersonation = !!session.impersonatorEmail;
  const organizationLabel = organization.name || organization.slug;

  let label = `Impersonating ${organization.slug}`;
  let actionLabel = "Stop impersonating";
  if (isDemo) {
    label = "Demo org — sample data";
    actionLabel = "Exit demo";
  } else if (isWorkOSImpersonation) {
    label = `WorkOS impersonation active — signed in as ${session.user.email} in ${organizationLabel}`;
  } else if (session.organizationOverride) {
    label = `Support access active for ${organizationLabel}`;
    actionLabel = "Exit support access";
  }

  const exit = () => {
    void (async () => {
      document.cookie = "gram_admin_override=; path=/; max-age=0;";
      // Exiting the demo switches back to the org the user came from (stashed
      // by /explore-demo), or their first real org — no logout round-trip.
      // Falls through to logout for support/WorkOS sessions and for a demo
      // user with no other organization.
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
      await logoutToLogin(client);
    })();
  };

  // Height must stay 2.25rem (h-9) to match --header-offset / --banner-offset.
  // Sticky so it stays above the fixed sidebar and sticky page header while
  // the document scrolls.
  // Solid ink-family bars (editorial): demo = ink, impersonation = deep brand
  // red. White mono label; the exit action is a hairline-outlined light chip.
  const toneClasses = isDemo
    ? "bg-surface-primary-fixed-dark border-transparent"
    : "bg-destructive-highlight border-transparent";
  const labelTone = "text-default-fixed-light";

  return (
    <div
      className={cn(
        "sticky top-0 z-40 flex h-9 items-center justify-center gap-3 border-b px-4",
        toneClasses,
      )}
      role="alert"
    >
      <ShieldAlert className={cn("h-3.5 w-3.5 shrink-0", labelTone)} />
      {/* Plain concatenation: tailwind-merge would treat text-eyebrow and the
          text-default-* tone as conflicting text-* utilities and drop one. */}
      <span className={`text-eyebrow min-w-0 truncate ${labelTone}`}>
        {label}
      </span>
      <button
        type="button"
        onClick={exit}
        className="border-neutral-softest text-default-fixed-light ml-2 shrink-0 border px-2 py-0.5 font-mono text-[11px] tracking-[0.08em] uppercase hover:bg-white/10"
      >
        {actionLabel}
      </button>
    </div>
  );
};

const AppLayoutContent = ({
  isImpersonating,
}: {
  isImpersonating: boolean;
}) => {
  const location = useLocation();
  // The assistant pages are a full-bleed canvas with their own header row;
  // the mode pill would sit on top of it.
  const isChat = useRoutes().chat.active;
  // Closing global settings returns here.
  useEffect(() => {
    rememberAppPath(location.pathname + location.search + location.hash);
  }, [location.pathname, location.search, location.hash]);

  return (
    <div className="relative flex min-h-screen w-full flex-col">
      {isImpersonating && <ImpersonationBanner />}
      {!isChat && <ModeSwitcher mode="canvas" />}
      <ModeSurface mode="canvas" className="flex w-full flex-1 overflow-x-clip">
        {/* Default (non-inset) variant: flat panes divided by a hairline
            instead of a floating bordered card. */}
        <AppSidebar />
        <SidebarInset>
          <GlobalInsightsWrapper>
            <MembershipSyncGuard>
              <Outlet />
            </MembershipSyncGuard>
          </GlobalInsightsWrapper>
        </SidebarInset>
        {/* Floats over the content rather than displacing it: opening a detail
            sheet used to reflow the page and move whatever had just been
            clicked. The page keeps its width; the panel covers its right edge. */}
        <SidePanelSurface />
      </ModeSurface>
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
            void logoutToLogin(client);
          }}
        >
          Log out
        </button>
      </div>
    </div>
  );
};

/**
 * Org-level pages are global settings, not a second app, so they open as a
 * full-screen overlay with their own chrome — no app sidebar, workspace
 * switcher or mode switcher — and close (X or Escape) back to the app page the
 * user came from.
 */
export const OrgLayout = (): JSX.Element => {
  const isImpersonating = useShowsImpersonationBanner();
  const organization = useOrganization();
  const navigate = useNavigate();

  const close = useCallback(() => {
    const landing = resolveLandingProject(organization.projects);
    void navigate(
      lastAppPath(organization.slug) ??
        (landing
          ? `/${organization.slug}/projects/${landing.slug}`
          : `/${organization.slug}/projects`),
    );
  }, [navigate, organization]);

  useEffect(() => {
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Escape" || event.defaultPrevented) return;
      // Escape belongs to whatever a page has open (a dialog, menu or
      // popover) or is being typed into; only a bare Escape closes settings.
      const target = event.target as HTMLElement | null;
      if (
        target?.closest("input, textarea, select, [contenteditable=true]") ||
        document.querySelector(
          "[role=dialog][data-state=open], [role=menu], [data-radix-popper-content-wrapper]",
        )
      ) {
        return;
      }
      close();
    };
    document.addEventListener("keydown", onKeyDown);
    return () => document.removeEventListener("keydown", onKeyDown);
  }, [close]);

  return (
    // Pages may reach for sidebar state (e.g. useSidebar), so the provider
    // stays even though no sidebar renders here.
    <SidebarProvider>
      <SettingsOverlayContext.Provider value={true}>
        <div className="bg-background fixed inset-0 z-40 flex items-center justify-center p-3 md:p-8">
          {/* Dimmed backdrop; clicking it closes, like any modal. */}
          <div
            aria-hidden="true"
            onClick={close}
            className="animate-in fade-in-0 absolute inset-0 bg-black/50 duration-200"
          />
          <div
            role="dialog"
            aria-modal="true"
            aria-labelledby="global-settings-title"
            className="animate-in fade-in-0 zoom-in-[0.98] bg-card border-border relative flex h-full w-full max-w-[1400px] flex-col overflow-hidden border shadow-2xl duration-200"
          >
            {isImpersonating && <ImpersonationBanner />}
            <button
              type="button"
              onClick={close}
              aria-label="Close settings"
              className="text-muted-foreground hover:text-foreground hover:bg-accent absolute top-4 right-4 z-30 flex size-9 items-center justify-center transition-colors"
            >
              <X className="size-5" />
            </button>
            <div className="flex min-h-0 flex-1 flex-col md:flex-row">
              <aside className="bg-background border-border shrink-0 overflow-y-auto border-b md:w-72 md:border-r md:border-b-0">
                <SettingsNav />
              </aside>
              <main
                className="relative min-w-0 flex-1 overflow-y-auto"
                // Pages pin toolbars below the app header; there is none here.
                style={{ "--page-sticky-top": "0px" } as React.CSSProperties}
              >
                <MembershipSyncGuard>
                  <Outlet />
                </MembershipSyncGuard>
              </main>
            </div>
          </div>
        </div>
      </SettingsOverlayContext.Provider>
    </SidebarProvider>
  );
};
