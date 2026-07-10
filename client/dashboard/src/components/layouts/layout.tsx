import * as React from "react";

import { Heading } from "@/components/ui/heading";
import { Type } from "@/components/ui/type";
import { cn } from "@/lib/utils";

/**
 * The shared page scaffold every page layout composes from. Encodes the
 * Claude Design page shape: a mono uppercase eyebrow, a display-serif
 * title, a quiet sans subtitle, right-aligned controls, and a hairline
 * rule closing the header band.
 *
 * Pages don't use `Layout` directly — they use one of the four page
 * layouts built on it (`ListLayout`, `DetailLayout`, `ObservabilityLayout`,
 * `SettingsLayout`), each of which re-exports these slots.
 */
function LayoutRoot({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}): JSX.Element {
  return <div className={cn("flex flex-col", className)}>{children}</div>;
}

/** Mono uppercase caption above the title (section, breadcrumb tail, kind). */
function LayoutEyebrow({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <span
      className={cn(
        "text-muted font-mono text-xs font-light tracking-[0.08em] uppercase",
        className,
      )}
    >
      {children}
    </span>
  );
}

/** The page title. Display serif, per the brand. */
function LayoutTitle({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Heading variant="h1" className={cn("normal-case", className)}>
      {children}
    </Heading>
  );
}

/** One quiet sans line under the title. Never more than a sentence. */
function LayoutSubtitle({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Type muted small className={className}>
      {children}
    </Type>
  );
}

/** Right-aligned header controls: range pickers, primary action, menus. */
function LayoutActions({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex shrink-0 items-center gap-2", className)}>
      {children}
    </div>
  );
}

/**
 * The header band: eyebrow + title + subtitle on the left, actions on the
 * right, closed by a hairline rule.
 */
function LayoutHeader({
  eyebrow,
  title,
  subtitle,
  actions,
  className,
}: {
  eyebrow?: React.ReactNode;
  title: React.ReactNode;
  subtitle?: React.ReactNode;
  actions?: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "border-neutral-softest flex items-end justify-between gap-6 border-b pb-6",
        className,
      )}
    >
      <div className="flex min-w-0 flex-col gap-2">
        {eyebrow ? <LayoutEyebrow>{eyebrow}</LayoutEyebrow> : null}
        <LayoutTitle>{title}</LayoutTitle>
        {subtitle ? <LayoutSubtitle>{subtitle}</LayoutSubtitle> : null}
      </div>
      {actions ? <LayoutActions>{actions}</LayoutActions> : null}
    </div>
  );
}

/** Page content below the header band. */
function LayoutBody({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return <div className={cn("flex flex-col pt-6", className)}>{children}</div>;
}

/**
 * A titled block inside the body. `title` is a display-serif section
 * heading; `annotation` is the mono count/summary that sits beside it.
 */
function LayoutSection({
  title,
  annotation,
  actions,
  children,
  className,
}: {
  title?: React.ReactNode;
  annotation?: React.ReactNode;
  actions?: React.ReactNode;
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <section className={cn("flex flex-col gap-4", className)}>
      {(title || actions) && (
        <div className="flex items-baseline justify-between gap-4">
          <div className="flex items-baseline gap-3">
            {title ? <Heading variant="h3">{title}</Heading> : null}
            {annotation ? <LayoutEyebrow>{annotation}</LayoutEyebrow> : null}
          </div>
          {actions ? <LayoutActions>{actions}</LayoutActions> : null}
        </div>
      )}
      {children}
    </section>
  );
}

// Compound members are attached by mutation rather than Object.assign: the
// react/only-export-components rule recognizes the former as a component
// export and flags the latter.
LayoutRoot.Header = LayoutHeader;
LayoutRoot.Eyebrow = LayoutEyebrow;
LayoutRoot.Title = LayoutTitle;
LayoutRoot.Subtitle = LayoutSubtitle;
LayoutRoot.Actions = LayoutActions;
LayoutRoot.Body = LayoutBody;
LayoutRoot.Section = LayoutSection;

export { LayoutRoot as Layout };
