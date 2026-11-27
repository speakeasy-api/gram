import { useHideInsightsDock } from "@/components/insights-context";
import { useAssistantsAvailable } from "@/hooks/useAssistantsAvailability";
import { Page } from "@/components/page-layout";
import { ProjectDashboard } from "@/components/project/ProjectDashboard";
import { RequireScope } from "@/components/require-scope";
import {
  BRAND_MESH_SURFACE_CLASS,
  BrandMeshLayers,
} from "@/components/brand-mesh";
import { cn } from "@/lib/utils";
import { ChatLanding } from "@/pages/chat/Chat";
import { useProject } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import { useRoutes } from "@/routes";
import { Navigate } from "react-router";
import { HomeWidgets } from "@/components/home/HomeWidgets";
import { type HomeWidgetId } from "@/components/home/home-widget-data";
import { HomeSectionLeads, HomeSections } from "@/components/home/HomeSections";
import { HomeTerminal } from "@/components/home/HomeTerminal";
import { SlotItem } from "@/components/home/home-widget-parts";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { Skeleton } from "@/components/ui/Skeleton";
import { ArrowDown } from "lucide-react";
import {
  useCallback,
  useEffect,
  useRef,
  useState,
  type CSSProperties,
  type RefObject,
} from "react";

export default function Home(): JSX.Element {
  const { hasAnyScope, hasAnyScopeInProject, isLoading } = useRBAC();
  const terminalHome = useFeatureFlag(FEATURE_FLAGS.terminalHome);
  const routes = useRoutes();
  const { id: projectId } = useProject();
  const assistantsAvailable = useAssistantsAvailable();
  // Home carries its own "Ask anything" widget, so suppress the floating dock.
  useHideInsightsDock();

  // Keep MCP-only users on the MCP page instead of rendering the project
  // overview's project:read fallback.
  if (
    !isLoading &&
    !hasAnyScope(["project:read"], projectId) &&
    hasAnyScopeInProject(["mcp:read", "mcp:write"], projectId)
  ) {
    return <Navigate to={routes.mcp.href()} replace />;
  }

  // Without the assistant there is nothing to anchor the grid, so those
  // projects keep the classic overview.
  const showTerminalHome =
    terminalHome.status === "enabled" &&
    assistantsAvailable &&
    hasAnyScope(["assistant:read"], projectId);

  if (terminalHome.status === "loading") {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Skeleton
            label="Loading the project home"
            className="h-[32rem] w-full"
          />
        </Page.Body>
      </Page>
    );
  }

  if (showTerminalHome) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        {/* The grid fills the viewport, so drop the default tall bottom
            padding that would otherwise leave a dead band under it. */}
        <Page.Body className="lg:pb-6">
          <RequireScope
            scope="project:read"
            resourceId={projectId}
            level="page"
          >
            <TerminalHomeGrid />
          </RequireScope>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <RequireScope scope="project:read" resourceId={projectId} level="page">
          {/* Full content width so the widget lines up with the dashboard
              below (the /chat page centers it; the home page does not). The
              compact variant drops pinned/recents — history lives on /chat. */}
          {assistantsAvailable && (
            <RequireScope
              scope="assistant:read"
              projectId={projectId}
              level="section"
            >
              <div className="w-full pt-2 pb-6">
                <div
                  className={cn(
                    BRAND_MESH_SURFACE_CLASS,
                    "border-border z-10 border p-8",
                  )}
                >
                  <BrandMeshLayers />
                  <ChatLanding compact />
                </div>
              </div>
            </RequireScope>
          )}
          <ProjectDashboard />
        </RequireScope>
      </Page.Body>
    </Page>
  );
}

const GRID_HEIGHT_CLASS =
  "lg:h-[calc(100dvh-var(--header-height)-var(--banner-offset,0px)-3.25rem)]";

/**
 * Layout units for the wide-screen grid: the slot under the chat takes 35%
 * of the first screen's height and the chat the rest.
 */
const GRID_VARS = {
  "--home-g":
    "calc(100dvh - var(--header-height) - var(--banner-offset, 0px) - 3.25rem)",
  "--home-u": "calc((var(--home-g) - 3rem) / 7)",
  "--home-top":
    "calc(var(--header-height) + var(--banner-offset, 0px) + 1.5rem)",
  "--home-slot-h": "calc((var(--home-g) - 1.5rem) * 0.35)",
  "--home-chat-h": "calc(var(--home-g) - var(--home-slot-h) - 1.5rem)",
  "--home-slot-top": "calc(var(--home-top) + var(--home-chat-h) + 1.5rem)",
} as CSSProperties;

/** The pinned slot under the chat that holds one widget at a time. */
const SLOT_CLASS =
  "relative hidden lg:sticky lg:top-[var(--home-slot-top)] lg:mt-6 lg:block lg:h-[var(--home-slot-h)]";
// The slot's first-screen widget, before any section scrolls up to it.
const FIRST_LEAD = "first";

/**
 * The terminal sits top-right and stays pinned as the page scrolls. Widgets
 * flow down the left lane beside it. Under the chat is a pinned slot that
 * always holds one whole widget: Biggest movers on the first screen, then
 * each product-area section's lead widget, crossfading in as its section
 * scrolls up to the slot. Nothing scrolls behind the chat.
 * Below `lg` everything stacks, terminal first.
 *
 * Expanding a widget switches to two columns: the left one stacks every
 * widget, the expanded one taking the free height and the rest collapsed to
 * their title bars; the terminal fills the right.
 */
function TerminalHomeGrid(): JSX.Element {
  const [expanded, setExpanded] = useState<HomeWidgetId | null>(null);
  const toggle = useCallback(
    (id: HomeWidgetId) => setExpanded((cur) => (cur === id ? null : id)),
    [],
  );
  const lane = useRef<HTMLDivElement>(null);
  const slot = useRef<HTMLDivElement>(null);
  const active = useActiveLead(lane, slot, expanded === null);

  if (expanded) {
    return (
      <div
        className={cn(
          "grid grid-cols-1 gap-6 pt-2 lg:grid-cols-12",
          GRID_HEIGHT_CLASS,
        )}
      >
        <HomeTerminal className="h-[32rem] lg:col-span-7 lg:col-start-6 lg:row-start-1 lg:h-auto" />
        <div className="flex min-h-0 flex-col gap-3 lg:col-span-5 lg:col-start-1 lg:row-start-1">
          <HomeWidgets
            classNames={{ [expanded]: "min-h-[24rem] lg:min-h-0 flex-1" }}
            expanded={expanded}
            onToggleExpand={toggle}
          />
        </div>
      </div>
    );
  }

  return (
    <div
      style={GRID_VARS}
      className="grid grid-cols-1 gap-6 pt-2 lg:grid-cols-12"
    >
      <div className="flex min-w-0 flex-col lg:col-span-7 lg:col-start-6 lg:row-start-1">
        <HomeTerminal className="h-[32rem] lg:sticky lg:top-[var(--home-top)] lg:h-[var(--home-chat-h)]" />
        <div ref={slot} className={SLOT_CLASS}>
          <SlotItem id={FIRST_LEAD} active={active === FIRST_LEAD}>
            <HomeWidgets ids={["movers"]} onToggleExpand={toggle} />
          </SlotItem>
          <HomeSectionLeads active={active} />
        </div>
      </div>
      <div
        ref={lane}
        className="flex min-w-0 flex-col gap-6 lg:col-span-5 lg:col-start-1 lg:row-start-1 lg:self-start"
      >
        <HomeWidgets
          ids={["attention", "spend", "movers", "active", "heatmap"]}
          classNames={{
            attention: "lg:h-[max(calc(var(--home-u)*3),16rem)]",
            movers: "lg:hidden",
            active: "lg:max-h-80",
          }}
          onToggleExpand={toggle}
        />
        <HomeSections />
        <ScrollHint />
      </div>
    </div>
  );
}

/**
 * Which slot widget shows: the lead of the last section whose heading has
 * scrolled up to the slot, or the first-screen widget before any has.
 */
function useActiveLead(
  lane: RefObject<HTMLDivElement | null>,
  slot: RefObject<HTMLDivElement | null>,
  enabled: boolean,
): string {
  const [active, setActive] = useState(FIRST_LEAD);
  useEffect(() => {
    const laneEl = lane.current;
    const slotEl = slot.current;
    if (!enabled || !laneEl || !slotEl) return;
    let frame = 0;
    const update = () => {
      frame = 0;
      const line = slotEl.getBoundingClientRect().top;
      let next = FIRST_LEAD;
      for (const section of laneEl.querySelectorAll<HTMLElement>(
        "[data-home-section]",
      )) {
        const id = section.dataset.homeSection;
        const hasLead = slotEl.querySelector(`[data-home-lead="${id}"]`);
        if (id && hasLead && section.getBoundingClientRect().top <= line) {
          next = id;
        }
      }
      setActive(next);
    };
    const schedule = () => {
      if (!frame) frame = requestAnimationFrame(update);
    };
    update();
    window.addEventListener("scroll", schedule, { passive: true });
    window.addEventListener("resize", schedule);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener("scroll", schedule);
      window.removeEventListener("resize", schedule);
    };
  }, [lane, slot, enabled]);
  return active;
}

/**
 * A fade pinned to the bottom of the widget column with a down arrow, so it
 * is clear more widgets sit below the first screen. It hides once the end of
 * the column is in view.
 */
function ScrollHint(): JSX.Element {
  const [atEnd, setAtEnd] = useState(false);
  const sentinel = useRef<HTMLDivElement>(null);
  useEffect(() => {
    const el = sentinel.current;
    // Without the API the hint just stays visible.
    if (!el || typeof IntersectionObserver === "undefined") return;
    const observer = new IntersectionObserver(([entry]) =>
      setAtEnd(entry?.isIntersecting ?? false),
    );
    observer.observe(el);
    return () => observer.disconnect();
  }, []);
  return (
    <>
      {/* Takes no room: it overlays the last widgets until the end shows. */}
      <div
        aria-hidden={atEnd}
        className={cn(
          "from-background/0 to-background pointer-events-none sticky bottom-0 -mt-30 flex h-24 items-end justify-center bg-gradient-to-b pb-3 transition-opacity",
          atEnd && "opacity-0",
        )}
      >
        <button
          type="button"
          aria-label="Scroll to more widgets"
          tabIndex={atEnd ? -1 : 0}
          onClick={() =>
            window.scrollBy({
              top: window.innerHeight * 0.7,
              behavior: "smooth",
            })
          }
          className={cn(
            "bg-card text-muted-foreground hover:text-foreground flex size-8 items-center justify-center rounded-full border",
            !atEnd && "pointer-events-auto",
          )}
        >
          <ArrowDown className="size-4" />
        </button>
      </div>
      <div ref={sentinel} aria-hidden className="-mt-6 h-px" />
    </>
  );
}
