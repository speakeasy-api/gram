import { useIconDither } from "@/components/icon-dither";
import { Page } from "@/components/page-layout";
import { StatusDot } from "@/components/ui/StatusDot";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Skeleton } from "@/components/ui/Skeleton";
import { scrollWithinPage } from "@/lib/scroll-within-page";
import { useIsDarkTheme } from "@/lib/theme";
import { cn } from "@/lib/utils";
import { Check, Circle, CircleDashed, ExternalLink } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router";
import { ReadinessIssuesProvider } from "@/components/detail/readiness-issues";
import type { McpDetailChrome, McpReadinessCheck } from "./mcpDetailChrome";

/** Link state that asks the frame to show the section a link lands on. */
const HIGHLIGHT_STATE = { highlightLinkedSection: true } as const;

/**
 * The frame every MCP detail page shares, servers and gateways alike: the
 * breadcrumb trail, a full-width header over the cards' halftone sea, the page
 * tabs on a white strip, then the active tab's content.
 *
 * The header's pattern fades out behind the title, so the name and its
 * controls always read on a clean ground in either theme.
 */
export function McpDetailFrame({
  chrome,
  breadcrumbs,
  children,
  contentRef,
}: {
  /** Null while the server or gateway loads. */
  chrome: McpDetailChrome | null;
  breadcrumbs: {
    substitutions: Record<string, string>;
    skipSegments: string[];
  };
  children: ReactNode;
  contentRef?: React.Ref<HTMLDivElement>;
}): JSX.Element {
  const activeTab = chrome?.items.find((item) => item.active)?.key;
  const { pathname } = useLocation();
  useHighlightLinkedSection();

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs
          substitutions={breadcrumbs.substitutions}
          skipSegments={breadcrumbs.skipSegments}
        />
      </Page.Header>

      {/* No body padding, so the header band and tab strip run edge to edge;
          each region pads itself to the same column as the tab content. */}
      {/* The body fills the viewport and only the tab content scrolls, so the
          header and tabs stay put on every tab. */}
      <Page.Body fullWidth noPadding overflowHidden className="gap-0">
        <HeaderBand>
          <div className="flex flex-wrap items-start justify-between gap-6">
            {chrome ? <TitleBlock chrome={chrome} /> : <TitleSkeleton />}
            {/* Settings has its own Plugins area and Overview leads with the
                full banner; one plugin box per view. */}
            {activeTab === "settings" ||
            (activeTab === "overview" && chrome?.overviewBanner)
              ? null
              : chrome?.aside}
          </div>
        </HeaderBand>

        <nav
          aria-label="Server sections"
          className="border-border bg-card shrink-0 border-b"
        >
          <div className="mx-auto flex w-full max-w-[1334px] gap-7 overflow-x-auto px-4 sm:px-8">
            {(chrome?.items ?? []).map((item) => (
              <Link
                key={item.key}
                to={item.href}
                aria-current={item.key === activeTab ? "page" : undefined}
                className={cn(
                  "relative flex h-12 shrink-0 items-center gap-1.5 text-sm transition-colors hover:no-underline",
                  item.key === activeTab
                    ? "text-foreground font-medium after:absolute after:inset-x-0 after:bottom-0 after:h-0.5 after:bg-current"
                    : "text-muted-foreground hover:text-foreground",
                )}
              >
                {item.titleNode ?? item.title}
              </Link>
            ))}
          </div>
        </nav>

        {/* data-page-bound marks this as the scroll container, so tab changes
            reset it to the top (useTabScrollReset). */}
        <div
          data-page-bound=""
          className="min-h-0 w-full flex-1 overflow-y-auto px-4 sm:px-8"
        >
          {/* Inspect gets a definite height, so it can fill the view and
              scroll inside itself. Other tabs grow with their content, so the
              bottom padding lands after the last card and clears the floating
              insights dock, which Inspect hides. */}
          <div
            ref={contentRef}
            className={cn(
              "mx-auto flex w-full max-w-[1270px] flex-col pt-8",
              activeTab === "inspect" ? "h-full pb-12" : "min-h-full pb-32",
            )}
          >
            <ReadinessIssuesProvider
              checks={(chrome?.readinessChecks ?? []).filter(
                (check) => !check.pending,
              )}
              pathname={pathname}
            >
              {activeTab === "overview" && chrome?.overviewSetup ? (
                <div className="mb-6">{chrome.overviewSetup}</div>
              ) : null}
              {activeTab === "overview" && chrome?.overviewBanner ? (
                // The banner renders nothing for viewers who can't manage
                // plugins, so the gap goes with it.
                <div className="mb-6 empty:hidden">{chrome.overviewBanner}</div>
              ) : null}
              {children}
            </ReadinessIssuesProvider>
          </div>
        </div>
      </Page.Body>
    </Page>
  );
}

/**
 * A full-bleed header with the cards' halftone sea on its right half. The sea
 * dissolves to white on the left, so the title and controls sit on a clean
 * ground instead of on tiles.
 */
function HeaderBand({ children }: { children: ReactNode }): JSX.Element {
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
      <div className="mx-auto w-full max-w-[1334px] px-4 pt-10 pb-8 sm:px-8">
        {children}
      </div>
    </header>
  );
}

function TitleBlock({ chrome }: { chrome: McpDetailChrome }): JSX.Element {
  return (
    <div className="flex max-w-[640px] min-w-0 flex-col gap-5">
      <div className="flex min-w-0 items-center gap-4">
        {chrome.icon("size-10 shrink-0 object-contain")}
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-eyebrow">
            {chrome.kindLabel ? `Connect · ${chrome.kindLabel}` : "Connect"}
          </span>
          {/* Long names wrap rather than run off a narrow screen. */}
          <h1 className="font-display text-[26px] leading-8 font-light tracking-tight break-words sm:text-[34px] sm:leading-9">
            {chrome.name}
          </h1>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
        {chrome.statusControl}
        <ReadinessChip chrome={chrome} />
        {chrome.primaryUrl ? (
          <span className="text-muted-foreground flex h-6 min-w-0 items-center gap-1">
            <span
              // Mono sits low beside the sans labels; a matching line box
              // keeps its centre on theirs.
              className="max-w-[320px] truncate font-mono text-xs leading-6"
              title={chrome.primaryUrl}
            >
              {chrome.primaryUrl.replace(/^https?:\/\//, "")}
            </span>
            <CopyButton text={chrome.primaryUrl} size="xs" tooltip="Copy URL" />
            {chrome.addresses ? (
              <AddressList addresses={chrome.addresses} />
            ) : null}
          </span>
        ) : null}
        {chrome.identity ? (
          <Link
            to={chrome.identity.href}
            aria-label={`Identity: ${chrome.identity.label}. Open identity settings`}
            className="text-muted-foreground hover:text-foreground flex h-6 items-center gap-1.5 leading-6 hover:no-underline"
          >
            <span className="text-eyebrow">Identity</span>
            <span
              className={cn(
                "text-foreground",
                chrome.identity.warn && "text-warning-foreground",
              )}
            >
              {chrome.identity.label}
            </span>
          </Link>
        ) : null}
        {chrome.installLinks.map((link) => (
          <a
            key={link.url}
            href={link.url}
            target="_blank"
            rel="noopener noreferrer"
            className="text-muted-foreground hover:text-foreground flex h-6 items-center gap-1 leading-6 hover:no-underline"
          >
            {link.label}
            <ExternalLink className="size-3.5" />
          </a>
        ))}
      </div>
    </div>
  );
}

/** The server's other addresses, one click from the primary one. */
function AddressList({
  addresses,
}: {
  addresses: { url: string; label: string }[];
}): JSX.Element {
  return (
    <Popover>
      <PopoverTrigger className="hover:text-foreground ml-1 text-xs leading-6 underline-offset-2 hover:underline">
        {`+${addresses.length - 1} more`}
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="w-auto max-w-[calc(100vw-2rem)] p-1"
      >
        <ul className="flex flex-col">
          {/* The first is the URL already shown beside the button. */}
          {addresses.slice(1).map((address) => (
            <li
              key={address.url}
              className="flex items-center justify-between gap-4 px-2 py-1.5"
            >
              <span className="flex min-w-0 flex-col">
                <span className="text-eyebrow">{address.label}</span>
                <span
                  className="truncate font-mono text-xs"
                  title={address.url}
                >
                  {address.url.replace(/^https?:\/\//, "")}
                </span>
              </span>
              <CopyButton text={address.url} size="xs" tooltip="Copy URL" />
            </li>
          ))}
        </ul>
      </PopoverContent>
    </Popover>
  );
}

/**
 * Quiet when the server is ready; otherwise names what is left and opens
 * the checklist, each item linking to where it's fixed. It waits for every
 * pending check, so it never names a step another check is about to change.
 */
function ReadinessChip({
  chrome,
}: {
  chrome: McpDetailChrome;
}): JSX.Element | null {
  const [open, setOpen] = useState(false);
  if (chrome.readinessChecks.length === 0) return null;
  const summary = summarizeReadiness(chrome.readinessChecks);
  const pendingNames = summary.open.map((check) => check.label).join(", ");

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger
        className="flex h-6 min-w-0 items-center gap-2 leading-6 whitespace-nowrap hover:underline"
        aria-label={
          summary.open.length > 0
            ? `${summary.label}: ${pendingNames}. Open checklist`
            : undefined
        }
        title={summary.open.length > 0 ? pendingNames : undefined}
      >
        <StatusDot tone={summary.tone} />
        <span
          className={cn(
            summary.open.length === 0
              ? "text-muted-foreground"
              : "text-foreground font-medium",
          )}
        >
          {summary.label}
        </span>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="flex w-[360px] max-w-[calc(100vw-2rem)] flex-col gap-1 p-2"
      >
        <span className="text-eyebrow px-2 pt-1 pb-2">Readiness</span>
        <ul className="flex flex-col">
          {chrome.readinessChecks.map((check) => (
            <li key={check.key}>
              <ReadinessRow
                check={check}
                settled={!summary.checking}
                onNavigate={() => setOpen(false)}
              />
            </li>
          ))}
        </ul>
        {/* The guide card hides itself when the provider has no guide, so it
            gets no rule of its own: an empty one would double the line. */}
        {chrome.setupGuide ? (
          <div className="px-2 empty:hidden">{chrome.setupGuide}</div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

/**
 * What the chip says. Every open item is counted, so the number always
 * matches the list: issues (broken now) and steps (not yet taken) together.
 */
function summarizeReadiness(checks: McpReadinessCheck[]): {
  label: string;
  tone: "destructive" | "warning" | "neutral" | "success";
  open: McpReadinessCheck[];
  checking: boolean;
} {
  const required = checks.filter((check) => !check.optional);
  if (required.some((check) => check.pending)) {
    return { label: "Checking…", tone: "neutral", open: [], checking: true };
  }
  const open = required.filter((check) => check.ready === false);
  const issues = open.filter((check) => check.broken).length;
  const [only] = open;
  if (!only) {
    // A check that can't be determined is neither a step left nor done.
    return required.some((check) => check.ready === null)
      ? { label: "Checking…", tone: "neutral", open, checking: false }
      : { label: "Ready", tone: "success", open, checking: false };
  }
  const tone = issues > 0 ? "destructive" : "warning";
  if (open.length === 1) {
    const label =
      only.chipLabel ??
      (only.broken ? "1 issue" : `1 step left: ${only.label}`);
    return { label, tone, open, checking: false };
  }
  const label =
    issues === open.length
      ? `${open.length} issues`
      : issues === 0
        ? `${open.length} steps left`
        : `${open.length} to fix`;
  return { label, tone, open, checking: false };
}

/** The row's state in words, so color is never the only signal. */
function rowState(
  check: McpReadinessCheck,
  settled: boolean,
): { tag: string | undefined; icon: ReactNode } {
  if (check.ready === true) {
    return {
      tag: undefined,
      icon: (
        <Check className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
      ),
    };
  }
  if (check.ready === null) {
    return {
      tag: check.pending ? "Checking" : undefined,
      icon: (
        <CircleDashed className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
      ),
    };
  }
  if (check.optional) {
    return {
      tag: "Optional",
      icon: (
        <Circle className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
      ),
    };
  }
  // Until every check settles, an open step isn't counted yet either.
  const tag = !settled ? undefined : check.broken ? "Issue" : "To do";
  return {
    tag,
    icon: (
      <Circle
        className={cn(
          "mt-0.5 size-3.5 shrink-0",
          check.broken
            ? "text-destructive"
            : "text-[var(--fill-warning-default)]",
        )}
      />
    ),
  };
}

function ReadinessRow({
  check,
  settled,
  onNavigate,
}: {
  check: McpReadinessCheck;
  settled: boolean;
  onNavigate: () => void;
}): JSX.Element {
  const { tag, icon } = rowState(check, settled);
  const body = (
    <span className="flex items-start gap-2.5 px-2 py-2">
      {icon}
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="text-foreground text-sm">{check.label}</span>
        <span
          className="text-muted-foreground text-xs leading-4"
          title={check.detail}
        >
          {check.description}
        </span>
      </span>
      {tag ? (
        <span
          className={cn(
            "mt-0.5 shrink-0 text-xs",
            tag === "Issue"
              ? "text-destructive"
              : tag === "To do"
                ? "text-foreground"
                : "text-muted-foreground",
          )}
        >
          {tag}
        </span>
      ) : null}
    </span>
  );
  // Every settled open check links, including one that couldn't be
  // determined: its target is where to try again.
  return check.href && check.ready !== true && !check.pending ? (
    <Link
      to={check.href}
      state={HIGHLIGHT_STATE}
      onClick={onNavigate}
      className="hover:bg-muted block hover:no-underline"
    >
      {body}
    </Link>
  ) : (
    body
  );
}

/**
 * After a readiness link lands, scrolls to the section its hash names and
 * flashes an outline round it, so the eye finds what to change. Tabs mount
 * their sections late, so it looks for the target for a couple of seconds.
 * With reduced motion the scroll jumps and the outline holds, then goes.
 */
function useHighlightLinkedSection(): void {
  const location = useLocation();
  const highlight =
    (location.state as { highlightLinkedSection?: boolean } | null)
      ?.highlightLinkedSection === true;
  const targetId = location.hash.replace(/^#/, "");

  useEffect(() => {
    if (!highlight || !targetId) return;
    const reduced =
      window.matchMedia?.("(prefers-reduced-motion: reduce)").matches ?? false;
    const behavior: ScrollBehavior = reduced ? "auto" : "smooth";
    let frame = 0;
    let tries = 0;
    const timers: number[] = [];
    let userScrolled = false;
    const stopFollowing = () => {
      userScrolled = true;
    };
    const find = () => {
      const target = document.getElementById(targetId);
      if (!target) {
        if (tries++ < 120) frame = requestAnimationFrame(find);
        return;
      }
      // Sections above can still be loading and push the target down, so
      // line it up again a few times, unless the viewer starts scrolling.
      const align = () => {
        if (!userScrolled) scrollWithinPage(target, { behavior });
      };
      align();
      timers.push(window.setTimeout(align, 400));
      timers.push(window.setTimeout(align, 1000));
      // Flash once the scroll has landed. The outline lives only in the
      // animation, so nothing is left behind.
      timers.push(
        window.setTimeout(() => {
          target.animate?.(
            [
              { outline: "2px solid var(--color-ring)", outlineOffset: "4px" },
              {
                outline: "2px solid var(--color-ring)",
                outlineOffset: "4px",
                offset: 0.6,
              },
              { outline: "2px solid transparent", outlineOffset: "4px" },
            ],
            { duration: 1600, easing: reduced ? "steps(1, end)" : "ease-out" },
          );
        }, 400),
      );
    };
    frame = requestAnimationFrame(find);
    const events = ["wheel", "touchmove", "keydown"] as const;
    for (const name of events) {
      window.addEventListener(name, stopFollowing, { passive: true });
    }
    return () => {
      cancelAnimationFrame(frame);
      for (const timer of timers) window.clearTimeout(timer);
      for (const name of events) {
        window.removeEventListener(name, stopFollowing);
      }
    };
    // location.key re-runs it for a second click on the same link.
  }, [highlight, targetId, location.key]);
}

function TitleSkeleton(): JSX.Element {
  return (
    <div className="flex items-center gap-4">
      <Skeleton className="size-10" />
      <div className="flex flex-col gap-2">
        <Skeleton className="h-3 w-28" />
        <Skeleton className="h-8 w-72" />
      </div>
    </div>
  );
}
