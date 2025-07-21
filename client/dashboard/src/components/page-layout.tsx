import { cn } from "@/lib/utils.ts";
import { useIsProjectEmpty } from "@/pages/onboarding/Onboarding.tsx";
import { useRoutes } from "@/routes.tsx";
import { Stack } from "@speakeasy-api/moonshine";
import { ContentErrorBoundary } from "./content-error-boundary.tsx";
import { PageHeader } from "./page-header.tsx";
import { Button } from "./ui/button.tsx";
import { Heading } from "./ui/heading.tsx";
import { Type } from "./ui/type.tsx";
import { XYFade } from "./ui/xy-fade.tsx";

function PageLayout({ children }: { children: React.ReactNode }) {
  return (
    // The height calculation accounts for the page body "visual" gutter
    <div className="h-[calc(100vh-16px)] flex flex-col overflow-hidden">
      {children}
    </div>
  );
}

function PageBody({
  children,
  className,
}: {
  children: React.ReactNode;
  className?: string;
}) {
  return (
    <div
      className={cn(
        "@container/main flex flex-col gap-4 p-8 pb-0 overflow-y-auto h-full",
        className
      )}
    >
      <ContentErrorBoundary>{children}</ContentErrorBoundary>
    </div>
  );
}

export const Page = Object.assign(PageLayout, {
  Header: PageHeader,
  Body: PageBody,
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
        Checking project...
      </Button>
    );
  } else if (!isEmpty && nonEmptyProjectCTA) {
    CTA = nonEmptyProjectCTA;
  }

  return (
    <div className="w-full h-full max-h-[600px] flex items-center justify-center bg-background rounded-xl border-1">
      <Stack
        gap={1}
        className="w-full max-w-sm"
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
