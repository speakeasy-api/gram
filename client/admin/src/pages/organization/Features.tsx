import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import type { JSX } from "react";

import { Switch } from "@/components/ui/switch";
import {
  organizationFeaturesQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import {
  errorMessage,
  setOrganizationFeature,
  type AdminOrganization,
  type AdminOrganizationFeatureName,
  type AdminOrganizationFeatures,
} from "@/lib/gramAdminApi";

type FeatureDefinition = {
  featureName: AdminOrganizationFeatureName;
  enabledKey: keyof AdminOrganizationFeatures;
  label: string;
  description: string;
};

const PRODUCT_FEATURES: FeatureDefinition[] = [
  {
    featureName: "authz_challenge_logging",
    enabledKey: "authz_challenge_logging_enabled",
    label: "Authz Challenge Logging",
    description:
      'Log every authorization decision (allow/deny) to ClickHouse. Powers auditing of "why did X have access to Y?"',
  },
  {
    featureName: "customer_managed_encryption_keys",
    enabledKey: "customer_managed_encryption_keys_enabled",
    label: "Customer-Managed Encryption Keys",
    description:
      "Unlocks encryption key management for an organization, enabling external service credential, external encryption key, and asymmetric signing functionality.",
  },
  {
    featureName: "custom_model_keys",
    enabledKey: "custom_model_keys_enabled",
    label: "Custom Model Provider Keys",
    description:
      "Allows projects in this organization to store OpenRouter API keys for model completions.",
  },
  {
    featureName: "platform_mcp",
    enabledKey: "platform_mcp_enabled",
    label: "Platform MCP access",
    description:
      "Allows this organization to authenticate to and use Platform MCP, including manual setup. Disabling it denies runtime access without removing existing setup records.",
  },
  {
    featureName: "remote_session_auto_refresh",
    enabledKey: "remote_session_auto_refresh_enabled",
    label: "Automatic Remote Session Refresh",
    description:
      "Shows the Auto refresh opt-in on remote-session consent screens.",
  },
  {
    featureName: "sso",
    enabledKey: "sso_enabled",
    label: "SSO",
    description: "Enables WorkOS portal link creation for managing SSO.",
  },
  {
    featureName: "scim",
    enabledKey: "scim_enabled",
    label: "SCIM",
    description: "Enables WorkOS portal link creation for managing SCIM.",
  },
];

export function FeaturesRoute(): JSX.Element | null {
  const { idOrSlug } = useParams({ from: "/organizations/$idOrSlug" });
  const { data } = useQuery(organizationQuery(idOrSlug));
  if (!data) return null;
  return <Features key={data.id} org={data} />;
}

export function Features({ org }: { org: AdminOrganization }): JSX.Element {
  const queryClient = useQueryClient();
  const query = organizationFeaturesQuery(org.id);
  const { data, isPending, isError } = useQuery({
    ...query,
    enabled: !!org.id,
  });
  const mutation = useMutation({
    mutationFn: ({
      featureName,
      enabled,
    }: {
      featureName: AdminOrganizationFeatureName;
      enabled: boolean;
    }) =>
      setOrganizationFeature({
        organizationID: org.id,
        featureName,
        enabled,
      }),
    onMutate: async ({ featureName, enabled }) => {
      await queryClient.cancelQueries({ queryKey: query.queryKey });
      const previous = queryClient.getQueryData<AdminOrganizationFeatures>(
        query.queryKey,
      );
      const definition = PRODUCT_FEATURES.find(
        (feature) => feature.featureName === featureName,
      );
      if (previous && definition) {
        queryClient.setQueryData<AdminOrganizationFeatures>(query.queryKey, {
          ...previous,
          [definition.enabledKey]: enabled,
        });
      }
      return {
        enabledKey: definition?.enabledKey,
        previousEnabled: definition
          ? previous?.[definition.enabledKey]
          : undefined,
      };
    },
    onError: async (_error, _variables, context) => {
      const enabledKey = context?.enabledKey;
      const previousEnabled = context?.previousEnabled;
      if (enabledKey && previousEnabled !== undefined) {
        queryClient.setQueryData<AdminOrganizationFeatures>(
          query.queryKey,
          (current) =>
            current
              ? {
                  ...current,
                  [enabledKey]: previousEnabled,
                }
              : current,
        );
      }
      await queryClient.invalidateQueries({ queryKey: query.queryKey });
    },
    onSuccess: (updated) => {
      queryClient.setQueryData(query.queryKey, updated);
    },
  });

  if (isPending) {
    return <span className="text-muted-foreground text-sm">Loading...</span>;
  }
  if (!data) {
    return (
      <span className="text-muted-foreground text-sm">
        Unable to load features
      </span>
    );
  }

  return (
    <>
      {isError && (
        <p className="text-muted-foreground mb-3 text-sm">
          Unable to refresh features; showing the last loaded state.
        </p>
      )}
      <section className="border-border overflow-hidden rounded-md border">
        <div className="border-border bg-muted/20 border-b px-4 py-3">
          <h5 className="text-sm font-medium">Product features</h5>
          <p className="text-muted-foreground text-sm">
            Org-level entitlements. Changes apply immediately to every member of
            the organization.
          </p>
        </div>
        <div className="divide-border divide-y">
          {PRODUCT_FEATURES.map((feature) => {
            const rowError =
              mutation.isError &&
              mutation.variables.featureName === feature.featureName
                ? errorMessage(mutation.error)
                : undefined;
            return (
              <div
                className="grid grid-cols-[1fr_auto] items-center gap-4 px-4 py-3"
                key={feature.featureName}
              >
                <div>
                  <p className="text-sm font-medium">{feature.label}</p>
                  <p className="text-muted-foreground text-sm">
                    {feature.description}
                  </p>
                  {rowError && (
                    <p className="text-destructive mt-1 text-sm">{rowError}</p>
                  )}
                </div>
                <Switch
                  checked={data[feature.enabledKey]}
                  disabled={mutation.isPending}
                  onCheckedChange={(enabled) =>
                    mutation.mutate({
                      featureName: feature.featureName,
                      enabled,
                    })
                  }
                  aria-label={`Toggle ${feature.label}`}
                />
              </div>
            );
          })}
        </div>
      </section>
    </>
  );
}
