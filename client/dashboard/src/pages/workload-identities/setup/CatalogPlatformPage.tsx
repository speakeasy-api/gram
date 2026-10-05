import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { SkeletonTable } from "@/components/ui/Skeleton";
import { useOrgRoutes } from "@/routes";
import { useWorkloadIdentities } from "@gram/client/react-query/workloadIdentities.js";
import { Plus } from "lucide-react";
import { Navigate, useParams, useSearchParams } from "react-router";
import { CatalogEmptyState, IssuerDetail } from "../WorkloadIssuerDetail";
import type { CatalogEntry } from "./definition";
import { PlatformSetupSheet } from "./PlatformSetupSheet";
import { PlatformTitle } from "./PlatformTitle";
import { connectedIssuer, useCatalogEntries } from "./platforms";

/** Query parameters naming the open setup step, so it survives a reload and can be linked to. */
const SETUP_PARAM = "setup";
const STEP_PARAM = "step";

/**
 * A catalog platform's page: the access already allowed under it, and the way
 * to register more, which is the platform's guided setup.
 */
export function CatalogPlatformPage(): JSX.Element {
  const { platformKey = "" } = useParams<{ platformKey: string }>();
  return (
    <RequireScope scope={["workload:read", "workload:write"]} level="page">
      <CatalogPlatform key={platformKey} platformKey={platformKey} />
    </RequireScope>
  );
}

function CatalogPlatform({
  platformKey,
}: {
  platformKey: string;
}): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const [searchParams, setSearchParams] = useSearchParams();
  const catalog = useCatalogEntries();
  const policy = useWorkloadIdentities({});
  const entry = catalog.entries.find(
    (candidate) => candidate.key === platformKey,
  );

  if (catalog.isPending || policy.isPending) {
    return (
      <ResourceListPage title="Catalog platform" stage="preview">
        <SkeletonTable />
      </ResourceListPage>
    );
  }

  // A stale link, or a platform the catalog no longer offers.
  if (entry === undefined || !entry.enabled || entry.setup === undefined) {
    return <Navigate to={orgRoutes.workloadIssuers.href()} replace />;
  }

  const issuer = connectedIssuer(entry, policy.data?.issuers ?? []);
  const setupOpen = searchParams.get(SETUP_PARAM) !== null;

  // Replaces rather than pushes: Back belongs to wherever the operator came
  // from, not to each step passed through.
  const setSetup = (open: boolean, stepId: string | null) => {
    setSearchParams(
      (previous) => {
        const params = new URLSearchParams(previous);
        if (open) {
          params.set(SETUP_PARAM, "");
        } else {
          params.delete(SETUP_PARAM);
        }
        if (stepId === null) {
          params.delete(STEP_PARAM);
        } else {
          params.set(STEP_PARAM, stepId);
        }
        return params;
      },
      { replace: true },
    );
  };

  const registerButton = (
    <RequireScope scope="workload:write" level="component">
      <Button size="sm" onClick={() => setSetup(true, null)}>
        <Button.LeftIcon>
          <Plus className="h-4 w-4" />
        </Button.LeftIcon>
        <Button.Text>Register new access</Button.Text>
      </Button>
    </RequireScope>
  );

  const setupButton = (
    <RequireScope scope="workload:write" level="component">
      <Button size="sm" onClick={() => setSetup(true, null)}>
        <Button.Text>Set one up</Button.Text>
      </Button>
    </RequireScope>
  );

  return (
    <>
      {issuer === undefined ? (
        <NotYetTrusted
          entry={entry}
          registerButton={registerButton}
          setupButton={setupButton}
        />
      ) : (
        <IssuerDetail
          issuerId={issuer.id}
          catalog={{
            title: <PlatformTitle name={entry.displayName} icon={entry.icon} />,
            name: entry.displayName,
            description: entry.description,
            registerButton,
            setupButton,
          }}
        />
      )}

      {setupOpen && (
        <PlatformSetupSheet
          entry={entry}
          definition={entry.setup}
          connected={issuer !== undefined}
          stepId={searchParams.get(STEP_PARAM)}
          onStepChange={(stepId) => setSetup(true, stepId)}
          onClose={() => setSetup(false, null)}
        />
      )}
    </>
  );
}

function NotYetTrusted({
  entry,
  registerButton,
  setupButton,
}: {
  entry: CatalogEntry;
  registerButton: JSX.Element;
  setupButton: JSX.Element;
}): JSX.Element {
  return (
    <ResourceListPage
      title={<PlatformTitle name={entry.displayName} icon={entry.icon} />}
      stage="preview"
      description={entry.description}
      primaryAction={registerButton}
    >
      <CatalogEmptyState catalog={{ name: entry.displayName, setupButton }} />
    </ResourceListPage>
  );
}
