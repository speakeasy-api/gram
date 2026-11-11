import { useIconDither } from "@/components/icon-dither";
import { Page } from "@/components/page-layout";
import type { ReadinessCheck } from "@/components/mcp-server-readiness-bar";
import { StatusDot } from "@/components/ui/StatusDot";
import { CopyButton } from "@/components/ui/CopyButton";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@/components/ui/Popover";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/utils";
import { Check, Circle, ExternalLink } from "lucide-react";
import { useEffect, type ReactNode } from "react";
import { Link } from "react-router";
import type { McpDetailChrome } from "./mcpDetailChrome";

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
            {chrome?.aside}
          </div>
        </HeaderBand>

        <nav
          aria-label="Server sections"
          className="border-border bg-card shrink-0 border-b"
        >
          <div className="mx-auto flex w-full max-w-[1334px] gap-7 overflow-x-auto px-8">
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
          className="min-h-0 w-full flex-1 overflow-y-auto px-8"
        >
          {/* A definite height, so a tab like Inspect can fill the view and
              scroll inside itself; taller tabs just overflow into the scroll. */}
          <div
            ref={contentRef}
            className="mx-auto flex h-full w-full max-w-[1270px] flex-col pt-8 pb-12"
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
  // Starts once laid out.
  useEffect(() => {
    const frame = requestAnimationFrame(start);
    return () => cancelAnimationFrame(frame);
  }, [start]);

  return (
    <header className="border-border bg-card relative isolate shrink-0 overflow-hidden border-b">
      <canvas
        ref={canvasRef}
        aria-hidden="true"
        className="pointer-events-none absolute inset-0 -z-10 size-full [mask-image:linear-gradient(to_right,transparent_28%,rgba(0,0,0,0.55)_58%,black_88%)]"
      />
      <div className="mx-auto w-full max-w-[1334px] px-8 pt-10 pb-8">
        {children}
      </div>
    </header>
  );
}

function TitleBlock({ chrome }: { chrome: McpDetailChrome }): JSX.Element {
  return (
    <div className="flex max-w-[640px] flex-col gap-5">
      <div className="flex min-w-0 items-center gap-4">
        {chrome.icon("size-10 shrink-0 object-contain")}
        <div className="flex min-w-0 flex-col gap-1.5">
          <span className="text-eyebrow">
            {chrome.kindLabel ? `Connect · ${chrome.kindLabel}` : "Connect"}
          </span>
          <h1 className="font-display truncate text-[34px] leading-9 font-light tracking-tight">
            {chrome.name}
          </h1>
        </div>
      </div>

      <div className="flex flex-wrap items-center gap-x-5 gap-y-2 text-sm">
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
    </div>
  );
}

/**
 * Quiet when the server is ready; otherwise names the steps left and opens
 * the checklist, each step linking to where it's fixed.
 */
function ReadinessChip({
  chrome,
}: {
  chrome: McpDetailChrome;
}): JSX.Element | null {
  const required = chrome.readinessChecks.filter((check) => !check.optional);
  if (chrome.readinessChecks.length === 0) return null;
  const left = required.filter((check) => check.ready !== true).length;

  return (
    <Popover>
      <PopoverTrigger className="flex h-6 items-center gap-2 leading-6 hover:underline">
        <StatusDot tone={left === 0 ? "success" : "warning"} />
        <span
          className={cn(
            left === 0
              ? "text-muted-foreground"
              : "text-foreground font-medium",
          )}
        >
          {left === 0
            ? "Ready"
            : `${left} ${left === 1 ? "step" : "steps"} left`}
        </span>
      </PopoverTrigger>
      <PopoverContent
        align="start"
        className="flex w-[360px] flex-col gap-1 p-2"
      >
        <span className="text-eyebrow px-2 pt-1 pb-2">Readiness</span>
        <ul className="flex flex-col">
          {chrome.readinessChecks.map((check) => (
            <li key={check.key}>
              <ReadinessRow check={check} />
            </li>
          ))}
        </ul>
        {chrome.identity ? (
          // In the popover the identity pill reads as text, not a boxed button.
          <div className="border-border mt-1 flex flex-col gap-2 border-t px-2 pt-3 pb-2 [&_button]:border-0 [&_button]:bg-transparent [&_button]:px-0">
            {chrome.identity}
          </div>
        ) : null}
        {/* The guide card hides itself when the provider has no guide, so it
            gets no rule of its own: an empty one would double the line. */}
        {chrome.setupGuide ? (
          <div className="px-2 empty:hidden">{chrome.setupGuide}</div>
        ) : null}
      </PopoverContent>
    </Popover>
  );
}

function ReadinessRow({ check }: { check: ReadinessCheck }): JSX.Element {
  const body = (
    <span className="flex items-start gap-2.5 px-2 py-2">
      {check.ready ? (
        <Check className="text-muted-foreground mt-0.5 size-3.5 shrink-0" />
      ) : (
        <Circle
          className={cn(
            "mt-0.5 size-3.5 shrink-0",
            check.optional
              ? "text-muted-foreground"
              : "text-warning-foreground",
          )}
        />
      )}
      <span className="flex min-w-0 flex-col">
        <span className="text-foreground text-sm">
          {check.label}
          {check.optional ? (
            <span className="text-muted-foreground"> · optional</span>
          ) : null}
        </span>
        <span className="text-muted-foreground text-xs leading-4">
          {check.description}
        </span>
      </span>
    </span>
  );
  return check.href && !check.ready ? (
    <Link to={check.href} className="hover:bg-muted block hover:no-underline">
      {body}
    </Link>
  ) : (
    body
  );
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
