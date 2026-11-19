import { useIconDither } from "@/components/icon-dither";
import { Page } from "@/components/page-layout";
import { Skeleton } from "@/components/ui/Skeleton";
import { useIsDarkTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { useEffect, type ReactNode } from "react";
import { Link } from "react-router";

/**
 * A header control's button: the visibility picker's look, so every control
 * in the header's right-hand column reads as the same kind of thing.
 */
export const DETAIL_HEADER_CONTROL_BUTTON =
  "text-foreground hover:bg-muted border-border bg-card flex h-8 w-full items-center gap-2 border px-3 text-sm font-medium whitespace-nowrap transition-colors hover:no-underline disabled:cursor-not-allowed disabled:opacity-50";

/** One tab in the strip under the header. */
export type DetailFrameTab = {
  key: string;
  title: string;
  titleNode?: ReactNode;
  href: string;
};

/**
 * The frame a resource's detail page shares: the breadcrumb trail, a
 * full-width header over the cards' halftone sea, the page tabs on a card
 * strip, then the active tab's content. The body fills the viewport and only
 * the tab content scrolls, so the header and tabs stay put on every tab.
 */
export function DetailFrame({
  breadcrumbs,
  header,
  tabs,
  activeTab,
  tabsLabel,
  strip,
  fill = false,
  contentRef,
  children,
}: {
  breadcrumbs: {
    substitutions: Record<string, string | undefined>;
    skipSegments?: string[];
    insertAfter?: { segment: string; display: string; url: string };
  };
  /** The header band's contents, usually a `DetailHeaderLayout`. */
  header: ReactNode;
  tabs: DetailFrameTab[];
  activeTab: string | undefined;
  /** Names the tab strip for assistive tech, e.g. "Server sections". */
  tabsLabel: string;
  /**
   * Replaces the tabs with other contents at the same height, for a page
   * that belongs to the resource but is not one of its tabs (e.g. Setup).
   */
  strip?: ReactNode;
  /**
   * Gives the content a definite height, so a tab can fill the view and
   * scroll inside itself. Other tabs grow with their content, and the bottom
   * padding clears the floating insights dock.
   */
  fill?: boolean;
  contentRef?: React.Ref<HTMLDivElement>;
  children: ReactNode;
}): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={breadcrumbs.substitutions}
          skipSegments={breadcrumbs.skipSegments}
          insertAfter={breadcrumbs.insertAfter}
        />
      </Page.Header>

      {/* No body padding, so the header band and tab strip run edge to edge;
          each region pads itself to the same column as the tab content. */}
      <Page.Body fullWidth noPadding overflowHidden className="gap-0">
        <DetailHeaderBand compact={fill}>{header}</DetailHeaderBand>

        <nav
          aria-label={tabsLabel}
          className="border-border bg-card shrink-0 border-b"
        >
          {strip !== undefined ? (
            <div className="mx-auto flex h-12 w-full max-w-[1334px] items-center gap-4 px-4 sm:px-8">
              {strip}
            </div>
          ) : (
            <div className="mx-auto flex w-full max-w-[1334px] gap-7 overflow-x-auto px-4 sm:px-8">
              {tabs.map((tab) => (
                <Link
                  key={tab.key}
                  to={tab.href}
                  aria-current={tab.key === activeTab ? "page" : undefined}
                  className={cn(
                    "relative flex h-12 shrink-0 items-center gap-1.5 text-sm transition-colors hover:no-underline",
                    tab.key === activeTab
                      ? "text-foreground font-medium after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-current"
                      : "text-muted-foreground hover:text-foreground",
                  )}
                >
                  {tab.titleNode ?? tab.title}
                </Link>
              ))}
            </div>
          )}
        </nav>

        {/* data-page-bound marks this as the scroll container, so tab changes
            reset it to the top (useTabScrollReset). */}
        <div
          data-page-bound=""
          className="min-h-0 w-full flex-1 overflow-y-auto px-4 sm:px-8"
        >
          <div
            ref={contentRef}
            className={cn(
              "mx-auto flex w-full max-w-[1270px] flex-col",
              // A filling tab is a workspace: on a short screen it keeps the
              // margins thin so the space goes to the tab itself.
              fill ? "short:py-4 h-full pt-8 pb-12" : "min-h-full pt-8 pb-32",
            )}
          >
            {children}
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}

/**
 * A full-bleed header with the cards' halftone sea on its right half. The sea
 * dissolves to the card color on the left, so the title and controls sit on a
 * clean ground instead of on tiles.
 */
function DetailHeaderBand({
  compact = false,
  children,
}: {
  /** Thinner padding on a short screen, for a tab that fills the view. */
  compact?: boolean;
  children: ReactNode;
}): JSX.Element {
  // A slow swell rather than the cards' hover shimmer.
  const { canvasRef, start } = useIconDither({
    speed: 0.18,
    stillWhenReduced: true,
    exclusive: false,
  });
  // The sea reads its ink when it starts, so a theme switch restarts it in
  // the new theme's color.
  const isDark = useIsDarkTheme();
  // Starts once laid out.
  useEffect(() => {
    const frame = requestAnimationFrame(start);
    return () => cancelAnimationFrame(frame);
  }, [start, isDark]);

  return (
    <header className="border-border bg-card relative isolate shrink-0 overflow-hidden border-b">
      <canvas
        ref={canvasRef}
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 size-full [mask-image:linear-gradient(to_right,transparent_28%,rgba(0,0,0,0.55)_58%,black_88%)]"
      />
      <div
        className={cn(
          "mx-auto w-full max-w-[1334px] px-4 sm:px-8",
          "pt-10 pb-8",
          compact && "short:py-4",
        )}
      >
        {children}
      </div>
    </header>
  );
}

/** The header's two columns: the title on the left, controls on the right. */
export function DetailHeaderLayout({
  title,
  controls,
}: {
  title: ReactNode;
  controls?: ReactNode;
}): JSX.Element {
  return (
    <div className="flex flex-wrap items-start justify-between gap-6">
      {title}
      <div className="flex w-full flex-col items-end gap-4 sm:w-auto sm:pt-1">
        {controls}
      </div>
    </div>
  );
}

/**
 * The header's title: an optional icon, an eyebrow, the name, and an
 * optional line under them. `name` may be a control, such as a picker, that
 * renders the name itself.
 */
export function DetailHeaderTitle({
  eyebrow,
  name,
  icon,
  below,
  compact = false,
}: {
  eyebrow: string;
  name: ReactNode;
  icon?: ReactNode;
  below?: ReactNode;
  /** On a short screen, drops the eyebrow and tightens the gaps. */
  compact?: boolean;
}): JSX.Element {
  return (
    <div
      className={cn(
        "flex max-w-[640px] min-w-0 flex-col",
        "gap-5",
        compact && "short:gap-2",
      )}
    >
      <div className="flex min-w-0 items-center gap-4">
        {icon}
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className={cn("text-eyebrow", compact && "short:hidden")}>
            {eyebrow}
          </span>
          {typeof name === "string" ? (
            <h1 className={DETAIL_HEADER_NAME}>{name}</h1>
          ) : (
            name
          )}
        </div>
      </div>
      {below}
    </div>
  );
}

/** The header name's type, for a control that renders the name itself. */
export const DETAIL_HEADER_NAME =
  // Long names wrap rather than run off a narrow screen.
  "font-display text-[26px] leading-8 font-light tracking-tight break-words sm:text-[34px] sm:leading-9";

/** Stands in for the title while the resource loads. */
export function DetailHeaderTitleSkeleton({
  icon = true,
}: {
  icon?: boolean;
}): JSX.Element {
  return (
    <div className="flex items-center gap-4">
      {icon ? <Skeleton className="size-10" /> : null}
      <div className="flex flex-col gap-2">
        <Skeleton className="h-3 w-28" />
        <Skeleton className="h-8 w-72" />
      </div>
    </div>
  );
}

export type DetailHeaderControlRow = {
  key: string;
  label: string;
  control: ReactNode;
};

/**
 * The header's controls, stacked on the right: a label, then a control in
 * the shared button look. Renders nothing without rows.
 */
export function DetailHeaderControls({
  rows,
  compact = false,
}: {
  rows: DetailHeaderControlRow[];
  /** Tighter rows on a short screen. */
  compact?: boolean;
}): JSX.Element | null {
  if (rows.length === 0) return null;
  return (
    // A soft wash of the card color behind the rows lifts their labels off
    // the halftone sea.
    <dl
      className={cn(
        "before:bg-card/85 relative isolate grid w-full grid-cols-[auto_minmax(0,1fr)] items-center gap-x-4 before:absolute before:-inset-y-5 before:-right-5 before:-left-12 before:-z-10 before:rounded-3xl before:blur-xl sm:w-[360px]",
        "gap-y-2",
        compact && "short:gap-y-1",
      )}
    >
      {rows.map((row) => (
        <div key={row.key} className="contents">
          <dt className="text-eyebrow">{row.label}</dt>
          <dd className="min-w-0">{row.control}</dd>
        </div>
      ))}
    </dl>
  );
}
