import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useParams } from "@tanstack/react-router";
import { useState, type JSX } from "react";

import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Switch } from "@/components/ui/switch";
import {
  organizationChatAnalysisSettingsQuery,
  organizationFeaturesQuery,
  organizationQuery,
} from "@/lib/adminQueries";
import {
  errorMessage,
  setOrganizationChatAnalysisSetting,
  setOrganizationFeature,
  triggerOrganizationChatAnalysis,
  type AdminChatAnalysisJudge,
  type AdminOrganization,
  type AdminOrganizationChatAnalysisSettings,
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
    featureName: "session_portability",
    enabledKey: "session_portability_enabled",
    label: "Session Portability",
    description:
      "Enables agent session portability for the device agent: session sharing links, move reporting with lineage, and picker title enrichment.",
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
  return (
    <div className="space-y-6">
      <ProductFeatures org={org} />
      <ChatAnalysis org={org} />
    </div>
  );
}

function ProductFeatures({ org }: { org: AdminOrganization }): JSX.Element {
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
    <div>
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
    </div>
  );
}

type ChatAnalysisDefinition = {
  judge: AdminChatAnalysisJudge;
  enabledKey: "work_units_enabled" | "business_memory_enabled";
  capKey: "work_units_daily_cap" | "business_memory_daily_cap";
  label: string;
  description: string;
  capLabel: string;
};

const CHAT_ANALYSIS_CONTROLS: ChatAnalysisDefinition[] = [
  {
    judge: "work_units",
    enabledKey: "work_units_enabled",
    capKey: "work_units_daily_cap",
    label: "Work Delivered Chat Analysis",
    description:
      "Evaluates work delivered in the organization's quiet chat sessions.",
    capLabel: "Work delivered daily evaluation cap",
  },
  {
    judge: "business_memory",
    enabledKey: "business_memory_enabled",
    capKey: "business_memory_daily_cap",
    label: "Business Memory Extraction",
    description:
      "Extracts reusable glossary entries, procedures, and prior results from quiet chat sessions.",
    capLabel: "Business memory daily extraction cap",
  },
];

function validDailyCap(value: string): number | undefined {
  if (value.trim() === "") return undefined;
  const cap = Number(value);
  return Number.isInteger(cap) && cap >= 0 && cap <= 10_000 ? cap : undefined;
}

function ChatAnalysis({ org }: { org: AdminOrganization }): JSX.Element {
  const queryClient = useQueryClient();
  const query = organizationChatAnalysisSettingsQuery(org.id);
  const { data, isPending, isError } = useQuery({
    ...query,
    enabled: !!org.id,
  });
  const [capOverrides, setCapOverrides] = useState<
    Record<AdminChatAnalysisJudge, string | null>
  >({
    work_units: null,
    business_memory: null,
  });

  const mutation = useMutation({
    mutationFn: ({
      judge,
      enabled,
      dailyCap,
    }: {
      judge: AdminChatAnalysisJudge;
      enabled: boolean;
      dailyCap: number;
    }) =>
      setOrganizationChatAnalysisSetting({
        organizationID: org.id,
        judge,
        enabled,
        dailyCap,
      }),
    onSuccess: (updated, variables) => {
      queryClient.setQueryData<AdminOrganizationChatAnalysisSettings>(
        query.queryKey,
        updated,
      );
      setCapOverrides((current) => ({
        ...current,
        [variables.judge]: null,
      }));
    },
  });
  const trigger = useMutation({
    mutationFn: (judge: AdminChatAnalysisJudge) =>
      triggerOrganizationChatAnalysis(org.id).then((result) => ({
        ...result,
        judge,
      })),
  });

  if (isPending) {
    return (
      <span className="text-muted-foreground text-sm">
        Loading chat analysis...
      </span>
    );
  }
  if (!data) {
    return (
      <span className="text-muted-foreground text-sm">
        Unable to load chat analysis settings
      </span>
    );
  }

  return (
    <div>
      {isError && (
        <p className="text-muted-foreground mb-3 text-sm">
          Unable to refresh chat analysis settings; showing the last loaded
          state.
        </p>
      )}
      <section className="border-border overflow-hidden rounded-md border">
        <div className="border-border bg-muted/20 border-b px-4 py-3">
          <h5 className="text-sm font-medium">Chat analysis</h5>
          <p className="text-muted-foreground text-sm">
            Caps are evaluations per UTC day; a cap of 0 disables the pipeline.
          </p>
        </div>
        <div className="divide-border divide-y">
          {CHAT_ANALYSIS_CONTROLS.map((control) => {
            const enabled = data[control.enabledKey];
            const capValue =
              capOverrides[control.judge] ?? String(data[control.capKey]);
            const cap = validDailyCap(capValue);
            const capDirty = cap !== undefined && cap !== data[control.capKey];
            const actionLabel = !enabled
              ? "Enable"
              : capDirty
                ? "Save cap"
                : "Disable";
            const rowError =
              mutation.isError && mutation.variables.judge === control.judge
                ? errorMessage(mutation.error)
                : undefined;
            const triggerError =
              trigger.isError && trigger.variables === control.judge
                ? errorMessage(trigger.error)
                : undefined;
            const triggerResult =
              trigger.isSuccess && trigger.data.judge === control.judge
                ? trigger.data
                : undefined;
            return (
              <div className="px-4 py-3" key={control.judge}>
                <div className="flex items-start justify-between gap-4">
                  <div>
                    <div className="flex items-center gap-2">
                      <p className="text-sm font-medium">{control.label}</p>
                      <Badge variant="outline">
                        {enabled ? "Enabled" : "Disabled"}
                      </Badge>
                    </div>
                    <p className="text-muted-foreground text-sm">
                      {control.description}
                    </p>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Input
                      className="w-28"
                      type="number"
                      min={0}
                      max={10_000}
                      step={1}
                      aria-label={control.capLabel}
                      value={capValue}
                      disabled={mutation.isPending}
                      onChange={(event) => {
                        setCapOverrides((current) => ({
                          ...current,
                          [control.judge]: event.target.value,
                        }));
                      }}
                    />
                    <Button
                      size="sm"
                      variant={enabled && !capDirty ? "destructive" : "default"}
                      disabled={mutation.isPending || cap === undefined}
                      onClick={() => {
                        if (cap === undefined) return;
                        const dailyCap = !enabled && cap === 0 ? 100 : cap;
                        if (dailyCap !== cap) {
                          setCapOverrides((current) => ({
                            ...current,
                            [control.judge]: String(dailyCap),
                          }));
                        }
                        mutation.mutate({
                          judge: control.judge,
                          enabled: !enabled || capDirty,
                          dailyCap,
                        });
                      }}
                    >
                      {actionLabel}
                    </Button>
                    {enabled && !capDirty && data[control.capKey] > 0 && (
                      <Button
                        size="sm"
                        variant="secondary"
                        disabled={trigger.isPending}
                        onClick={() => trigger.mutate(control.judge)}
                      >
                        {trigger.isPending &&
                        trigger.variables === control.judge
                          ? "Running…"
                          : "Run now"}
                      </Button>
                    )}
                  </div>
                </div>
                {cap === undefined && (
                  <p className="text-destructive mt-1 text-sm">
                    Cap must be a whole number from 0 to 10,000.
                  </p>
                )}
                {rowError && (
                  <p className="text-destructive mt-1 text-sm">{rowError}</p>
                )}
                {triggerError && (
                  <p className="text-destructive mt-1 text-sm">
                    {triggerError}
                  </p>
                )}
                {triggerResult && (
                  <p className="text-muted-foreground mt-1 text-sm">
                    Triggered {triggerResult.projects_signaled} project
                    {triggerResult.projects_signaled === 1 ? "" : "s"}.
                  </p>
                )}
              </div>
            );
          })}
        </div>
      </section>
    </div>
  );
}
