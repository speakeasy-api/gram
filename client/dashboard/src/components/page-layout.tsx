// oxlint-disable react/only-export-components -- compound component (Object.assign) pattern
import { cn } from "@/lib/utils.ts";
import { useRoutes } from "@/routes.tsx";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
import React, { ReactElement } from "react";
import { Link } from "react-router";
import { ContentErrorBoundary } from "./content-error-boundary.tsx";
import { PageHeader } from "./page-header.tsx";
import { ReleaseStage, ReleaseStageBadge } from "./release-stage-badge.tsx";
import { Heading } from "@/components/ui/Heading";
import { MoreActions } from "@/components/ui/MoreActions";
import { Toolbar } from "@/components/ui/Toolbar";
import { Text } from "@/components/ui/Text";
import { XYFade } from "@/components/ui/XyFade";
import { PageEyebrow } from "./page-eyebrow";

function PageLayout({ children }: { children: React.ReactNode }) {
  return (
    // Height accounts for the SidebarInset visual gutter (m-2 top+bottom = 1rem)
    // and the impersonation banner via --banner-offset. The top bar is gone, so
    // there's no header/pt-2 term to subtract.
    <div className="flex h-[calc(100vh-1rem-var(--banner-offset,0px))] flex-col overflow-hidden">
      <ContentErrorBoundary>{children}</ContentErrorBoundary>
    </div>
  );
}

function PageBody({
  children,
  fullWidth = false,
  fullHeight = false,
  noPadding = false,
  overflowHidden = false,
  className,
}: {
  children: React.ReactNode;
  fullWidth?: boolean;
  fullHeight?: boolean;
  noPadding?: boolean;
  overflowHidden?: boolean;
  className?: string;
}) {
  return (
    // Nest the max-width container inside another div so that the entire page area remains scrollable
    <div
      // Anchor for useTabScrollReset: the one scroll container a page owns.
      data-page-scroll=""
      className={cn(
        // flex-1 + min-h-0 ensures this pane occupies exactly the remaining
        // space in PageLayout's flex column (after PageHeader). Using h-full
        // here would resolve to 100% of PageLayout and overflow past the
        // header, clipping content at the bottom.
        "min-h-0 w-full flex-1",
        overflowHidden ? "flex flex-col overflow-hidden" : "overflow-y-auto",
      )}
    >
      <div
        className={cn(
          "@container/main flex w-full flex-col gap-4",
          noPadding ? "p-0" : "p-8",
          !noPadding && "pb-24",
          !fullWidth && "mx-auto max-w-7xl",
          fullHeight && "h-full",
          overflowHidden && "min-h-0 flex-1",
          className,
        )}
      >
        {children}
      </div>
    </div>
  );
}

type PageSectionChild =
  | ReactElement<typeof PageSection.Title>
  | ReactElement<typeof PageSection.Description>
  | ReactElement<typeof PageSection.CTA>
  | ReactElement<typeof PageSection.Body>
  | ReactElement<typeof PageSection.MoreActions>
  | null;

function PageSectionComponent({ children }: { children: PageSectionChild[] }) {
  const slots = {
    title: null as React.ReactElement | null,
    description: null as React.ReactElement | null,
    ctas: [] as React.ReactElement[],
    body: null as React.ReactElement | null,
    moreActions: null as React.ReactElement | null,
  };

  // Process children to extract slots by checking component type
  React.Children.forEach(children, (child) => {
    if (React.isValidElement(child)) {
      // Check if the child is one of our slot components
      if (child.type === PageSectionTitle) {
        slots.title = child;
      } else if (child.type === PageSectionDescription) {
        slots.description = child;
      } else if (child.type === PageSectionCTA) {
        slots.ctas.push(child);
      } else if (child.type === PageSectionBody) {
        slots.body = child;
      } else if (child.type === PageSection.MoreActions) {
        slots.moreActions = child;
      }
    }
  });

  return (
    <Stack gap={2} className="mt-3 mb-6">
      {/* Render header with title, description, and CTA if they exist */}
      {(slots.title || slots.description || slots.ctas.length > 0) && (
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-6"
        >
          <Stack gap={2} className="min-w-0">
            {slots.title}
            {slots.description}
          </Stack>
          <Stack
            direction="horizontal"
            gap={2}
            align="center"
            className="shrink-0"
          >
            {slots.ctas.map((cta) => cta)}
            {slots.moreActions}
          </Stack>
        </Stack>
      )}
      {/* Render body */}
      {slots.body}
    </Stack>
  );
}

function PageSectionTitle({
  children,
  className,
  stage,
  area,
}: {
  children: React.ReactNode;
  className?: string;
  stage?: ReleaseStage;
  /** Override the auto-derived area eyebrow; pass "" to suppress it. */
  area?: string;
}) {
  // Primary page title: area eyebrow + thin display serif (Tobias) per the
  // editorial idiom. text-display-sm carries the font family, thin weight,
  // tight tracking, and display text color token; font-thin re-asserts weight
  // over Heading's font-normal base.
  const titleClassName = cn("text-display-sm font-thin", className);

  const title = stage ? (
    <Stack direction="horizontal" align="center" gap={2}>
      <Heading variant="h3" className={titleClassName}>
        {children}
      </Heading>
      <ReleaseStageBadge stage={stage} />
    </Stack>
  ) : (
    <Heading variant="h3" className={titleClassName}>
      {children}
    </Heading>
  );

  return (
    <Stack gap={2}>
      {area === "" ? null : <PageEyebrow area={area} />}
      {title}
    </Stack>
  );
}

function PageSectionDescription({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Text muted small className={cn("font-normal", className)}>
      {children}
    </Text>
  );
}

function PageSectionBody({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

function PageSectionCTA({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

const PageSection = Object.assign(PageSectionComponent, {
  Title: PageSectionTitle,
  Description: PageSectionDescription,
  Body: PageSectionBody,
  CTA: PageSectionCTA,
  MoreActions: MoreActions,
});

function PageBanner({ children }: { children: React.ReactNode }) {
  return <div className="flex w-full shrink-0 flex-col">{children}</div>;
}

export const Page = Object.assign(PageLayout, {
  Header: PageHeader,
  Banner: PageBanner,
  Body: PageBody,
  Section: PageSection,
  Toolbar: Toolbar,
  Eyebrow: PageEyebrow,
});

export function EmptyState({
  heading,
  description,
  nonEmptyProjectCTA,
  graphic,
  graphicClassName,
}: {
  heading: string;
  description: string;
  nonEmptyProjectCTA?: React.ReactNode;
  graphic: React.ReactNode;
  graphicClassName?: string;
}): React.JSX.Element {
  const routes = useRoutes();

  const CTA: React.ReactNode = nonEmptyProjectCTA ?? (
    <Button asChild size="sm">
      <Link to={routes.catalog.href()}>Browse catalog</Link>
    </Button>
  );

  return (
    <div className="bg-background flex h-[600px] w-full items-center justify-center border">
      <Stack
        gap={1}
        className="m-8 w-full max-w-sm"
        align="center"
        justify="center"
      >
        <XYFade
          className={cn("h-[250px] w-full overflow-hidden", graphicClassName)}
          fadeColor="var(--background)"
        >
          {graphic}
        </XYFade>
        <Heading variant="h5" className="font-medium">
          {heading}
        </Heading>
        <Text small muted className="mb-4 text-center">
          {description}
        </Text>
        {CTA}
      </Stack>
    </div>
  );
}
