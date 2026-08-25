import {
  MODES,
  useModeSwitch,
  type Mode,
} from "@/components/mode-switch-stage";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { useFeatureFlag } from "@/hooks/useFeatureFlag";
import { FEATURE_FLAGS } from "@/lib/featureFlags";
import { useOrgRoutes } from "@/routes";
import { useEffect } from "react";
import { useLocation } from "react-router";

// Where the user was working before switching to headless mode, so the
// Dashboard tab returns them to that page rather than dropping them at the org
// home. Session-scoped: a new tab starts without a remembered page.
const CANVAS_PATH_KEY = "gram.mode-switcher.canvas-path";

function rememberCanvasPath(path: string): void {
  try {
    sessionStorage.setItem(CANVAS_PATH_KEY, path);
  } catch {
    // Storage disabled — the Dashboard tab falls back to the org home.
  }
}

function rememberedCanvasPath(): string | null {
  try {
    return sessionStorage.getItem(CANVAS_PATH_KEY);
  } catch {
    return null;
  }
}

/**
 * Whether the chrome shows the mode switcher. The layouts need this too: with
 * the strip hidden they must not reserve its height in the offsets below.
 */
export function useModeSwitcherEnabled(): boolean {
  return (
    useFeatureFlag(FEATURE_FLAGS.headlessModeSwitcher).status === "enabled"
  );
}

// The sidebar is fixed-positioned from --header-offset, and pages size
// themselves against --banner-offset, so the strip's height has to be a known
// constant both layouts can add into those offsets. Kept in sync with
// STRIP_HEIGHT_PX in mode-switch-stage.tsx.
export const MODE_SWITCHER_HEIGHT = "3rem";

// Fixed segment width so the sliding indicator can be positioned by index
// instead of measured from the DOM.
const SEGMENT_WIDTH_REM = 7.5;

function ModeSegment({
  label,
  icon,
  active,
  href,
  onSelect,
}: {
  label: string;
  icon: IconName;
  active: boolean;
  href: string;
  onSelect: (href: string) => void;
}) {
  // Plain concatenation, not cn(): tailwind-merge reads text-eyebrow and the
  // text-* tone as conflicting text utilities and drops the eyebrow.
  const tone = active
    ? "text-default-fixed-light"
    : "text-muted-foreground hover:text-foreground";

  return (
    // An anchor, not a button: each mode is a real URL, so it stays
    // middle-clickable and copyable while a plain click runs the animated
    // in-app switch.
    <a
      href={href}
      aria-current={active ? "page" : undefined}
      onClick={(event) => {
        // Modified clicks (new tab/window) stay with the browser.
        if (event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) {
          return;
        }
        event.preventDefault();
        if (!active) onSelect(href);
      }}
      className={`text-eyebrow relative z-10 flex h-7 items-center justify-center gap-1.5 rounded-full transition-colors duration-200 ${tone}`}
      style={{ width: `${SEGMENT_WIDTH_REM}rem` }}
    >
      <Icon name={icon} className="h-3 w-3" />
      {label}
    </a>
  );
}

/**
 * Out-of-canvas mode switcher at the very top of the app chrome, spanning the
 * full window above both the sidebar and the content pane. Swaps between the
 * normal dashboard and headless mode (Platform MCP setup, no sidebar).
 */
export function ModeSwitcher({ mode }: { mode: Mode }): JSX.Element | null {
  const orgRoutes = useOrgRoutes();
  const location = useLocation();
  const { switchTo, phase } = useModeSwitch();
  const enabled = useModeSwitcherEnabled();
  const path = location.pathname + location.search;

  useEffect(() => {
    if (mode === "canvas") rememberCanvasPath(path);
  }, [mode, path]);

  // Rollout control only: the headless route stays reachable by URL, it just
  // has no chrome-level entry point until the flag is on.
  if (!enabled) return null;

  const hrefs: Record<Mode, string> = {
    canvas:
      mode === "canvas"
        ? path
        : (rememberedCanvasPath() ?? orgRoutes.home.href()),
    headless: orgRoutes.headless.href(),
  };
  const activeIndex = MODES.findIndex((entry) => entry.mode === mode);

  return (
    <nav
      aria-label="Interface mode"
      // Transparent so the mesh behind the whole chrome shows through. The
      // hairline only earns its place against an opaque pane: in headless mode
      // and mid-switch the mesh runs on below the strip, and a rule across it
      // reads as a seam.
      className={`relative z-30 flex h-12 shrink-0 items-center justify-center ${
        mode === "canvas" && phase === "idle" ? "border-border border-b" : ""
      }`}
    >
      <div className="border-border bg-card/80 relative flex items-center rounded-full border p-0.5 backdrop-blur-sm">
        {/* Solid ink pill that slides between segments — the one moving part,
            so the swap reads as a physical toggle rather than a repaint. */}
        <span
          aria-hidden="true"
          className="bg-surface-primary-fixed-dark absolute inset-y-0.5 left-0.5 rounded-full transition-transform duration-300 ease-out"
          style={{
            width: `${SEGMENT_WIDTH_REM}rem`,
            transform: `translateX(${activeIndex * SEGMENT_WIDTH_REM}rem)`,
          }}
        />
        {MODES.map((entry) => (
          <ModeSegment
            key={entry.mode}
            label={entry.label}
            icon={entry.icon}
            active={entry.mode === mode}
            href={hrefs[entry.mode]}
            onSelect={(href) => switchTo(mode, entry.mode, href)}
          />
        ))}
      </div>
    </nav>
  );
}
