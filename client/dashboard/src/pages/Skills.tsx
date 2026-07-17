import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import { Button } from "@/components/ui/button";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useProject } from "@/contexts/Auth";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Badge, Icon } from "@speakeasy-api/moonshine";
import { Outlet } from "react-router";

export default function Skills(): JSX.Element {
  const { id: projectId } = useProject();
  const {
    data: features,
    error,
    isLoading,
    refetch,
  } = useProductFeatures(undefined, undefined, {
    staleTime: 30_000,
    throwOnError: false,
  });

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <SkillsFeatureGate
          error={error}
          isLoading={isLoading || (!error && features === undefined)}
          skillsEnabled={features?.skillsEnabled}
          projectId={projectId}
          onRetry={() => void refetch()}
        />
      </Page.Body>
    </Page>
  );
}

function SkillsFeatureGate({
  error,
  isLoading,
  skillsEnabled,
  projectId,
  onRetry,
}: {
  error: Error | null;
  isLoading: boolean;
  skillsEnabled: boolean | undefined;
  projectId: string;
  onRetry: () => void;
}): JSX.Element {
  if (isLoading) {
    return <SkillsGateLoading />;
  }

  // A failed background refresh should not hide an already-resolved gate.
  if (error && skillsEnabled === undefined) {
    return (
      <div className="mx-auto mt-8 flex max-w-xl flex-col gap-3">
        <ErrorAlert
          title="Unable to load Skills availability"
          error="Refresh the page or try again."
        />
        <Button variant="outline" className="self-start" onClick={onRetry}>
          Try again
        </Button>
      </div>
    );
  }

  if (skillsEnabled === false) {
    return (
      <RequireScope scope="project:read" level="page">
        <SkillsComingSoon />
      </RequireScope>
    );
  }

  return (
    <RequireScope scope="skill:read" resourceId={projectId} level="page">
      <Outlet />
    </RequireScope>
  );
}

function SkillsGateLoading(): JSX.Element {
  return (
    <div aria-label="Loading Skills" className="mt-3 space-y-4">
      <Skeleton className="h-7 w-36" />
      <Skeleton className="h-4 w-full max-w-xl" />
      <Skeleton className="h-64 w-full" />
    </div>
  );
}

function SkillsComingSoon(): JSX.Element {
  return (
    <Page.Section>
      <Page.Section.Title>Skills</Page.Section.Title>
      <Page.Section.Description>
        Build and distribute skills with your team. Track usage, enable
        discovery and improve performance.
      </Page.Section.Description>
      <Page.Section.Body>
        <div className="bg-muted/20 flex flex-col items-center justify-center rounded-xl border border-dashed px-8 py-16">
          <div className="bg-muted/50 mb-4 flex h-12 w-12 items-center justify-center rounded-full">
            <Icon name="terminal" className="text-muted-foreground h-6 w-6" />
          </div>
          <Type variant="subheading" className="mb-1">
            No skills yet
          </Type>
          <Type small muted className="max-w-md text-center">
            Build and distribute skills to your team. Track usage, enable
            discovery and improve performance.
          </Type>
          <Badge variant="neutral" className="mt-3">
            Coming Soon
          </Badge>
        </div>
      </Page.Section.Body>
    </Page.Section>
  );
}
