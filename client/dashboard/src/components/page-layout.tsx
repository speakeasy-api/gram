import { cn } from "@/lib/utils.ts";
import { useIsProjectEmpty } from "@/pages/onboarding/Onboarding.tsx";
import { useRoutes } from "@/routes.tsx";
import { Stack } from "@speakeasy-api/moonshine";
import React from "react"; // Added missing import for React
import { ContentErrorBoundary } from "./content-error-boundary.tsx";
import { PageHeader } from "./page-header.tsx";
import { Button } from "@speakeasy-api/moonshine";
import { Heading } from "./ui/heading.tsx";
import { MoreActions } from "./ui/more-actions.tsx";
import { Type } from "./ui/type.tsx";
import { XYFade } from "./ui/xy-fade.tsx";

function PageLayout({ children }: { children: React.ReactNode }) {
  return (
    // The height calculation accounts for the page body "visual" gutter
    <div className="h-[calc(100vh-16px)] flex flex-col overflow-hidden">
      <ContentErrorBoundary>{children}</ContentErrorBoundary>
    </div>
  );
}

function PageBody({
  children,
  fullWidth = false,
  className,
}: {
  children: React.ReactNode;
  fullWidth?: boolean;
  className?: string;
}) {
  return (
    // Nest the max-width container inside another div so that the entire page area remains scrollable
    <div className="overflow-y-auto h-full w-full">
      <div
        className={cn(
          "@container/main flex flex-col gap-4 p-8 pb-0 h-full w-full",
          !fullWidth && "max-w-7xl mx-auto ",
          className
        )}
      >
        {children}
      </div>
    </div>
  );
}

function PageSectionComponent({ children }: { children: React.ReactNode }) {
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
    <Stack gap={2} className="mb-8 mt-3">
      {/* Render header with title, description, and CTA if they exist */}
      {(slots.title || slots.description || slots.ctas.length > 0) && (
        <Stack
          direction="horizontal"
          justify="space-between"
          align="center"
          className="mb-4"
        >
          <Stack gap={2}>
            {slots.title}
            {slots.description}
          </Stack>
          <Stack direction="horizontal" gap={2} align="center">
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
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <Heading variant="h3" className={className}>
      {children}
    </Heading>
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
    <Type muted small className={cn("font-normal", className)}>
      {children}
    </Type>
  );
}

function PageSectionBody({ children }: { children: React.ReactNode }) {
  return <>{children}</>;
}

function PageSectionCTA({
  children,
  ...props
}: { children: React.ReactNode } & React.ComponentProps<typeof Button>) {
  return <Button {...props}>{children}</Button>;
}

const PageSection = Object.assign(PageSectionComponent, {
  Title: PageSectionTitle,
  Description: PageSectionDescription,
  Body: PageSectionBody,
  CTA: PageSectionCTA,
  MoreActions: MoreActions,
});

export const Page = Object.assign(PageLayout, {
  Header: PageHeader,
  Body: PageBody,
  Section: PageSection,
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
}) {
  const routes = useRoutes();
  const { isEmpty, isLoading } = useIsProjectEmpty();

  let CTA: React.ReactNode = (
    <routes.onboarding.Link>
      <Button size="sm">Get Started</Button>
    </routes.onboarding.Link>
  );

  if (isLoading) {
    CTA = (
      <Button disabled size="sm">
        CHECKING PROJECT...
      </Button>
    );
  } else if (!isEmpty && nonEmptyProjectCTA) {
    CTA = nonEmptyProjectCTA;
  }

  return (
    <div className="w-full h-[600px] flex items-center justify-center bg-background rounded-xl border-1">
      <Stack
        gap={1}
        className="w-full max-w-sm m-8"
        align="center"
        justify="center"
      >
        <XYFade
          className={cn("w-full h-[250px]", graphicClassName)}
          fadeColor="var(--background)"
        >
          {graphic}
        </XYFade>
        <Heading variant="h5" className="font-medium">
          {heading}
        </Heading>
        <Type small muted className="mb-4 text-center">
          {description}
        </Type>
        {CTA}
      </Stack>
    </div>
  );
}
