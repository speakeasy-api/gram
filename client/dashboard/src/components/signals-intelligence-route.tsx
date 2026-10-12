import { InlineEmptyState } from "@/components/inline-empty-state";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { useOrganization, useProject } from "@/contexts/Auth";
import SignalsIntelligence from "@/pages/sigint/SignalsIntelligence";
import { useRoutes } from "@/routes";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { Navigate } from "react-router";

export default function SignalsIntelligenceRoute(): JSX.Element {
  const project = useProject();
  return (
    <RequireScope scope="project:read" resourceId={project.id} level="page">
      <SignalsIntelligenceAccess />
    </RequireScope>
  );
}

function SignalsIntelligenceAccess() {
  const organization = useOrganization();
  const project = useProject();
  const routes = useRoutes();
  const features = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { throwOnError: false },
  );

  if (features.isPending || features.isError || !features.data) {
    return (
      <ResourceListPage
        title="Signals intelligence"
        stage="preview"
        isLoading={features.isPending}
      >
        <InlineEmptyState
          icon="triangle-alert"
          heading="Could not check feature access"
          description="Retry to check whether Signals intelligence is available to your organization."
          action={
            <Button variant="secondary" onClick={() => void features.refetch()}>
              Try again
            </Button>
          }
        />
      </ResourceListPage>
    );
  }

  if (!features.data.signalsIntelligenceEnabled) {
    return <Navigate to={routes.home.href()} replace />;
  }

  return <SignalsIntelligence key={`${organization.id}:${project.id}`} />;
}
