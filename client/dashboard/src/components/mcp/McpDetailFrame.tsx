import {
  DETAIL_HEADER_CONTROL_BUTTON,
  DetailFrame,
  DetailHeaderControls,
  DetailHeaderLayout,
  DetailHeaderTitle,
  DetailHeaderTitleSkeleton,
  type DetailHeaderControlRow,
} from "@/components/detail/detail-frame";
import { StatusDot } from "@/components/ui/StatusDot";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { scrollWithinPage } from "@/lib/scroll-within-page";
import { cn } from "@/lib/utils";
import {
  Check,
  ChevronDown,
  ChevronRight,
  Circle,
  CircleDashed,
  ExternalLink,
} from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { Link, useLocation } from "react-router";
import { ReadinessIssuesProvider } from "@/components/detail/readiness-issues";
import type { McpDetailChrome, McpReadinessCheck } from "./mcpDetailChrome";

/** Link state that asks the frame to show the section a link lands on. */
const HIGHLIGHT_STATE = { highlightLinkedSection: true } as const;

/**
 * The frame every MCP detail page shares, servers and gateways alike: the
 * shared detail frame with the server's name, URL and controls in the header.
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
  const { pathname } = useLocation();
  // The Setup modal opens over Overview, which stays the active tab.
  const activeTab =
    chrome?.items.find((item) => item.active)?.key ??
    (chrome?.setupHref && pathname === chrome.setupHref
      ? "overview"
      : undefined);
  useHighlightLinkedSection();

  return (
    <DetailFrame
      breadcrumbs={breadcrumbs}
      tabsLabel="Server sections"
      tabs={chrome?.items ?? []}
      activeTab={activeTab}
      // Inspect fills the view and scrolls inside itself; it also hides the
      // floating insights dock the other tabs clear.
      fill={activeTab === "inspect"}
      contentRef={contentRef}
      header={
        <DetailHeaderLayout
          title={
            chrome ? (
              <TitleBlock chrome={chrome} />
            ) : (
              <DetailHeaderTitleSkeleton />
            )
          }
          controls={chrome ? <HeaderControls chrome={chrome} /> : null}
        />
      }
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
    </DetailFrame>
  );
}

function TitleBlock({ chrome }: { chrome: McpDetailChrome }): JSX.Element {
  return (
    <DetailHeaderTitle
      icon={chrome.icon("size-10 shrink-0 object-contain")}
      eyebrow={chrome.kindLabel ? `Connect · ${chrome.kindLabel}` : "Connect"}
      name={chrome.name}
      below={
        chrome.primaryUrl || chrome.installLinks.length > 0 ? (
          <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
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
                <CopyButton
                  text={chrome.primaryUrl}
                  size="xs"
                  tooltip="Copy URL"
                />
                {chrome.addresses ? (
                  <AddressList addresses={chrome.addresses} />
                ) : null}
              </span>
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
        ) : undefined
      }
    />
  );
}

/**
 * The header's controls: readiness, how it authenticates to the upstream
 * server, and whether agents can connect.
 */
function HeaderControls({
  chrome,
}: {
  chrome: McpDetailChrome;
}): JSX.Element | null {
  const rows: DetailHeaderControlRow[] = [];
  if (chrome.readinessChecks.length > 0) {
    rows.push({
      key: "readiness",
      label: "Readiness",
      control: <ReadinessChip chrome={chrome} />,
    });
  }
  if (chrome.identity) {
    rows.push({
      key: "identity",
      label: "Upstream auth",
      control: (
        <Link
          to={chrome.identity.href}
          state={HIGHLIGHT_STATE}
          aria-label={`Upstream auth: ${chrome.identity.label}. Open authentication settings`}
          className={DETAIL_HEADER_CONTROL_BUTTON}
        >
          {chrome.identity.tone ? (
            <StatusDot tone={chrome.identity.tone} />
          ) : null}
          <span
            className={cn(
              "min-w-0 flex-1 truncate text-left",
              chrome.identity.warn && "text-warning-foreground",
            )}
          >
            {chrome.identity.label}
          </span>
          <ChevronRight className="text-muted-foreground size-3.5 shrink-0" />
        </Link>
      ),
    });
  }
  if (chrome.statusControl) {
    rows.push({
      key: "status",
      label: "Status",
      control: (
        // The visibility picker sizes itself to fit; here it fills the row.
        <div className="[&>button]:h-8 [&>button]:w-full">
          {chrome.statusControl}
        </div>
      ),
    });
  }
  return <DetailHeaderControls rows={rows} />;
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
        className={DETAIL_HEADER_CONTROL_BUTTON}
        aria-label={
          summary.open.length > 0
            ? `${summary.label}: ${pendingNames}. Open checklist`
            : undefined
        }
        title={summary.open.length > 0 ? pendingNames : undefined}
      >
        <StatusDot tone={summary.tone} />
        <span className="min-w-0 flex-1 truncate text-left">
          {summary.label}
        </span>
        <ChevronDown className="text-muted-foreground size-3.5 shrink-0" />
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
      // Once the scroll has landed, a hairline square border settles around
      // the section, holds, then fades. A section already showing a
      // readiness issue is framed by it, so it gets no second border. The
      // border lives only in the animation, so nothing is left behind.
      const line =
        "color-mix(in oklab, var(--color-foreground) 22%, transparent)";
      const clear =
        "color-mix(in oklab, var(--color-foreground) 0%, transparent)";
      const ringAt = (color: string) => ({
        outline: `1px solid ${color}`,
        outlineOffset: "8px",
      });
      timers.push(
        window.setTimeout(() => {
          if (target.querySelector("[data-readiness-issue]")) return;
          target.animate?.(
            [
              ringAt(clear),
              { ...ringAt(line), offset: 0.12 },
              { ...ringAt(line), offset: 0.75 },
              ringAt(clear),
            ],
            { duration: 2400, easing: reduced ? "steps(1, end)" : "ease-out" },
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
