import { cn } from "@/lib/utils";
import type React from "react";

export interface LogWorkbenchProps {
  title: React.ReactNode;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  filters?: React.ReactNode;
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
  title,
  description,
  actions,
  filters,
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
}: LogWorkbenchProps) {
  return (
    <>
      <div className={cn("flex min-h-0 w-full flex-1 flex-col", className)}>
        <div className="shrink-0 px-8 py-4">
          <div className="mb-4 flex items-start justify-between gap-4">
            <div className="flex min-w-0 flex-col gap-1">
              <h1 className="text-xl font-semibold">{title}</h1>
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

        <div className="min-h-0 flex-1 overflow-hidden border-t">
          <div
            className={cn(
              "bg-background flex h-full flex-col",
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
