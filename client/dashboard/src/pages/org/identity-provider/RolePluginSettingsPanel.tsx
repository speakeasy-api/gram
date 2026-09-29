import { useState } from "react";
import { useOrganization } from "@/contexts/Auth";
import { Button } from "@/components/ui/Button";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildRoleProvisioningQuery } from "@gram/client/react-query/roleProvisioning.js";
import { useConfigureRoleProvisioningMutation } from "@gram/client/react-query/configureRoleProvisioning.js";
import {
  RolePluginSettings,
  type RolePluginStatus,
} from "./RolePluginSettings";

export function RolePluginSettingsPanel({
  preferredProjectSlug,
}: {
  preferredProjectSlug?: string;
}): JSX.Element {
  const organization = useOrganization();
  return (
    <SettingsLoader
      key={organization.id}
      organizationId={organization.id}
      preferredProjectId={
        organization.projects.find(
          (project) => project.slug === preferredProjectSlug,
        )?.id
      }
    />
  );
}
function SettingsLoader({
  preferredProjectId,
  organizationId,
}: {
  preferredProjectId?: string;
  organizationId: string;
}) {
  const client = useGramContext();
  const queryClient = useQueryClient();
  const options = buildRoleProvisioningQuery(
    client,
    { gramSession: "" },
    { sessionHeaderGramSession: "" },
  );
  const query = useQuery({
    ...options,
    queryKey: [...options.queryKey, { organizationId }],
    throwOnError: false,
    retry: false,
    refetchOnWindowFocus: false,
  });

  const [revision, setRevision] = useState(0);
  const [error, setError] = useState<string>();
  const mutation = useConfigureRoleProvisioningMutation({
    retry: false,
    onSuccess: (result) => {
      queryClient.setQueryData(
        [...options.queryKey, { organizationId }],
        result,
      );
      setError(undefined);
      setRevision((value) => value + 1);
    },
    onError: (cause) => {
      setError(
        "statusCode" in cause && cause.statusCode === 409
          ? "These settings changed in another session. Reload saved settings, review your selections, and save again."
          : "Could not save role plugin settings. Your selections have been kept. Try again or reload saved settings.",
      );
    },
  });
  // Refetch promises resolve before React Query's batched subscriber render.
  // Read the authoritative cache synchronously when resetting the editor so
  // its draft and expected version always capture the same fresh response.
  const data =
    queryClient.getQueryData<RolePluginStatus>([
      ...options.queryKey,
      { organizationId },
    ]) ?? query.data;
  if (!data) {
    return (
      <section aria-label="Automatic role plugins" className="space-y-3">
        {query.isPending ? (
          <p role="status">Loading role plugin settings…</p>
        ) : (
          <>
            <p role="alert">
              Could not load role plugin settings. Directory sync is unaffected.
            </p>
            <Button variant="secondary" onClick={() => void query.refetch()}>
              Retry role plugin settings
            </Button>
          </>
        )}
      </section>
    );
  }
  return (
    <RolePluginSettings
      key={revision}
      status={data}
      preferredProjectId={preferredProjectId}
      saving={mutation.isPending || query.isFetching}
      error={error}
      onSave={(configuration) => {
        setError(undefined);
        mutation.mutate({
          request: { configureRoleProvisioningRequestBody: configuration },
          security: { sessionHeaderGramSession: "" },
        });
      }}
      onReload={() => {
        void query.refetch().then((result) => {
          if (result.data && !result.error) {
            setError(undefined);
            setRevision((value) => value + 1);
          } else {
            setError(
              "Could not reload saved settings. Your selections have been kept.",
            );
          }
        });
      }}
    />
  );
}
