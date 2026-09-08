import { cn } from "@/lib/utils";
import { PageEyebrow } from "@/components/page-eyebrow";
import {
  ReleaseStageBadge,
  type ReleaseStage,
} from "@/components/release-stage-badge";
import type React from "react";

export interface LogWorkbenchProps {
  /** Mono uppercase micro-label rendered as an overline above the title. */
  eyebrow?: React.ReactNode;
  title: React.ReactNode;
  stage?: ReleaseStage;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  filters?: React.ReactNode;
  /** Full-width row (e.g. a summary chart) between the filters and the log surface. */
  summary?: React.ReactNode;
  status?: React.ReactNode;
  header?: React.ReactNode;
  children: React.ReactNode;
  footer?: React.ReactNode;
  detail?: React.ReactNode;
  onScroll?: React.UIEventHandler<HTMLDivElement>;
  scrollRef?: React.Ref<HTMLDivElement>;
  surfaceClassName?: string;
  contentClassName?: string;
  className?: string;
}

export function LogWorkbench({
  eyebrow,
  title,
  stage,
  description,
  actions,
  filters,
  summary,
  status,
  header,
  children,
  footer,
  detail,
  onScroll,
  scrollRef,
  surfaceClassName,
  contentClassName,
  className,
}: LogWorkbenchProps): JSX.Element {
  return (
    <>
      <div
        className={cn(
          "flex min-h-0 w-full flex-1 flex-col gap-6 px-8 pt-8",
          className,
        )}
      >
        <div className="shrink-0">
          <div className="mb-4 flex items-start justify-between gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <PageEyebrow area={eyebrow} />
              <div className="flex items-center gap-2">
                {/* Page title: thin display serif per the editorial idiom. */}
                <h1 className="text-display-sm font-thin">{title}</h1>
                {stage ? <ReleaseStageBadge stage={stage} /> : null}
              </div>
              {description ? (
                <p className="text-muted-foreground text-sm">{description}</p>
              ) : null}
            </div>
            {actions ? (
              <div className="flex shrink-0 items-center gap-2">{actions}</div>
            ) : null}
          </div>
          {filters ? (
            <div className="flex flex-wrap items-center gap-4">{filters}</div>
          ) : null}
        </div>

        {summary ? <div className="shrink-0">{summary}</div> : null}

        {/* Inset the table in a bordered card so it doesn't run full-bleed to
            the page edges — matching the Tool Logs page layout. */}
        <div className="flex min-h-0 flex-1 overflow-hidden">
          <div
            className={cn(
              "bg-background flex h-full min-h-0 flex-1 flex-col border",
              surfaceClassName,
            )}
          >
            {status}
            {header}
            <div
              ref={scrollRef}
              className={cn("flex-1 overflow-y-auto", contentClassName)}
              onScroll={onScroll}
            >
              {children}
            </div>
            {footer}
          </div>
        </div>
      </div>

      {detail}
    </>
  );
}
