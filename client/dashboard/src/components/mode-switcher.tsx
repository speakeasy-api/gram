import {
  MODES,
  useModeSwitch,
  useModeSwitcherEnabled,
  type Mode,
} from "@/components/mode-switch-context";
import { Icon } from "@/components/ui/Icon";
import type { IconName } from "@/components/ui/Icon/names";
import { cn } from "@/lib/utils";
import { useOrgRoutes } from "@/routes";
import { useSlugs } from "@/contexts/Sdk";
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

function rememberedCanvasPath(orgSlug: string | undefined): string | null {
  if (!orgSlug) return null;
  try {
    const path = sessionStorage.getItem(CANVAS_PATH_KEY);
    if (!path) return null;
    // The store is per tab and the active organization can change within one, so
    // a path belonging to another organization must not be restored. Match on a
    // whole segment: a prefix test would accept "/acme-staging/..." for "acme".
    const rest = path.slice(`/${orgSlug}`.length);
    const sameOrg =
      path.startsWith(`/${orgSlug}`) &&
      (rest === "" || "/?#".includes(rest[0] ?? ""));
    return sameOrg ? path : null;
  } catch {
    return null;
  }
}

// Fixed segment width so the sliding indicator can be positioned by index
// instead of measured from the DOM.
const SEGMENT_WIDTH_REM = 7.5;

function ModeSegment({
  label,
  icon,
  active,
  onInk,
  href,
  onSelect,
}: {
  label: string;
  icon: IconName;
  active: boolean;
  onInk: boolean;
  href: string;
  onSelect: (href: string) => void;
}) {
  // Plain concatenation, not cn(): tailwind-merge reads text-eyebrow and the
  // text-* tone as conflicting text utilities and drops the eyebrow.
  // The pill is always the inverse of the bar, so the type flips with it.
  const tone = onInk
    ? active
      ? "text-default-fixed-dark"
      : "text-muted-fixed-light hover:text-default-fixed-light"
    : active
      ? "text-default-inverse"
      : "text-muted hover:text-default";

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
      className={`text-eyebrow relative z-10 flex h-7 items-center justify-center gap-1.5 rounded-full transition-colors duration-500 ${tone}`}
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
  const { orgSlug } = useSlugs();
  const location = useLocation();
  const { switchTo, phase } = useModeSwitch();
  const enabled = useModeSwitcherEnabled();
  const path = location.pathname + location.search + location.hash;

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
        : (rememberedCanvasPath(orgSlug) ?? orgRoutes.home.href()),
    headless: orgRoutes.headless.href(),
  };
  const activeIndex = MODES.findIndex((entry) => entry.mode === mode);
  // Ink while a switch is in flight (the tab grid is behind it) and while
  // headless mode is mounted; light over the dashboard at rest.
  const onInk = mode === "headless" || phase !== "idle";

  return (
    <nav
      aria-label="Interface mode"
      // Read by computeGrid to measure where the panes actually start.
      data-mode-switcher=""
      // The bar matches whatever sits under it: light over the dashboard, ink
      // once the tab grid or headless mode is showing, so the chrome and the
      // starfield read as one dark surface.
      className={cn(
        "relative z-30 flex h-14 shrink-0 items-center justify-center transition-colors duration-500",
        onInk
          ? "bg-surface-tertiary-fixed-dark"
          : "bg-background border-border border-b",
      )}
    >
      <div
        className={cn(
          "relative flex items-center rounded-full border p-0.5 transition-colors duration-500",
          onInk ? "border-neutral-softest" : "border-border",
        )}
      >
        {/* Solid ink pill that slides between segments — the one moving part,
            so the swap reads as a physical toggle rather than a repaint. */}
        <span
          aria-hidden="true"
          className={cn(
            "absolute inset-y-0.5 left-0.5 rounded-full transition-colors duration-500",
            // Off the ink the pill follows the theme — a fixed-dark pill is
            // invisible on a dark-mode bar.
            onInk
              ? "bg-surface-primary-fixed-light"
              : "bg-surface-primary-inverse",
          )}
          style={{
            width: `${SEGMENT_WIDTH_REM}rem`,
            transform: `translateX(${activeIndex * SEGMENT_WIDTH_REM}rem)`,
            // Slow enough to read as the pill travelling between segments, on
            // the same curve the pane animation uses.
            // The shorthand replaces the class-level transition-colors, so the
            // ink/light swap has to be listed here too or it snaps.
            transition:
              "transform 620ms cubic-bezier(0.32, 0.72, 0, 1), background-color 500ms ease",
          }}
        />
        {MODES.map((entry) => (
          <ModeSegment
            key={entry.mode}
            label={entry.label}
            icon={entry.icon}
            active={entry.mode === mode}
            onInk={onInk}
            href={hrefs[entry.mode]}
            onSelect={(href) => switchTo(mode, entry.mode, href)}
          />
        ))}
      </div>
    </nav>
  );
}
