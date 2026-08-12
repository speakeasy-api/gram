import { Page } from "@/components/page-layout";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Switch } from "@/components/ui/Switch";
import { useOrgMemoryDeveloperToggle } from "@/hooks/useOrgMemoryDeveloperToggle";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import {
  invalidateAllChatAnalysisSettings,
  useChatAnalysisSettings,
} from "@gram/client/react-query/chatAnalysisSettings.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import {
  invalidateAllProductFeatures,
  useProductFeatures,
} from "@gram/client/react-query/productFeatures.js";
import { useTriggerChatAnalysisMutation } from "@gram/client/react-query/triggerChatAnalysis.js";
import { useUpsertBusinessMemoryAnalysisSettingsMutation } from "@gram/client/react-query/upsertBusinessMemoryAnalysisSettings.js";
import { useUpsertChatAnalysisSettingsMutation } from "@gram/client/react-query/upsertChatAnalysisSettings.js";
import { useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { toast } from "sonner";
import { AdminRow, AdminSection } from "./AdminSection";
import { PlatformAdminGate } from "./PlatformAdminGate";

export default function PlatformAdminFeatures(): JSX.Element {
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <Page.Section>
          <Page.Section.Title area="Platform Admin">
            Features
          </Page.Section.Title>
          <Page.Section.Description>
            Product feature entitlements and analysis pipelines for this
            organization.
          </Page.Section.Description>
          <Page.Section.Body>
            <PlatformAdminGate>
              <div className="space-y-8">
                <ProductFeaturesSection />
                <ChatAnalysisSection />
                <DeveloperSection />
              </div>
            </PlatformAdminGate>
          </Page.Section.Body>
        </Page.Section>
      </Page.Body>
    </Page>
  );
}

const PRODUCT_FEATURES: {
  featureName: FeatureName;
  label: string;
  description: string;
  enabledKey:
    | "authzChallengeLoggingEnabled"
    | "customerManagedEncryptionKeysEnabled"
    | "remoteSessionAutoRefreshEnabled"
    | "ssoEnabled"
    | "scimEnabled";
}[] = [
  {
    featureName: FeatureName.AuthzChallengeLogging,
    label: "Authz Challenge Logging",
    description:
      'Log every authorization decision (allow/deny) to ClickHouse. Powers auditing of "why did X have access to Y?"',
    enabledKey: "authzChallengeLoggingEnabled",
  },
  {
    featureName: FeatureName.CustomerManagedEncryptionKeys,
    label: "Customer-Managed Encryption Keys",
    description:
      "Unlocks encryption key management for an organization, enabling external service credential, external encryption key, and asymmetric signing functionality.",
    enabledKey: "customerManagedEncryptionKeysEnabled",
  },
  {
    featureName: FeatureName.RemoteSessionAutoRefresh,
    label: "Automatic Remote Session Refresh",
    description:
      "Shows the Auto refresh opt-in on remote-session consent screens.",
    enabledKey: "remoteSessionAutoRefreshEnabled",
  },
  {
    featureName: FeatureName.Sso,
    label: "SSO",
    description: "Enables WorkOS portal link creation for managing SSO.",
    enabledKey: "ssoEnabled",
  },
  {
    featureName: FeatureName.Scim,
    label: "SCIM",
    description: "Enables WorkOS portal link creation for managing SCIM.",
    enabledKey: "scimEnabled",
  },
];

function ProductFeaturesSection(): JSX.Element {
  const queryClient = useQueryClient();
  const { data: features, isLoading, error } = useProductFeatures();

  const {
    mutate,
    isPending,
    error: mutError,
    variables,
  } = useFeaturesSetMutation({
    onSuccess: () => {
      void invalidateAllProductFeatures(queryClient);
    },
  });

  const pendingFeature =
    variables?.request?.setProductFeatureRequestBody?.featureName;

  const body = () => {
    if (isLoading) {
      return (
        <p className="text-muted-foreground px-4 py-3 text-sm">
          Loading feature flags…
        </p>
      );
    }
    if (error || !features) {
      return (
        <p className="text-destructive px-4 py-3 text-sm">
          Failed to load feature flags: {error?.message ?? "unknown error"}
        </p>
      );
    }
    return (
      <div className="divide-border divide-y">
        {PRODUCT_FEATURES.map((feature) => {
          const enabled = features[feature.enabledKey];
          const rowPending =
            isPending && pendingFeature === feature.featureName;
          const rowError =
            pendingFeature === feature.featureName
              ? mutError?.message
              : undefined;
          return (
            <AdminRow
              key={feature.featureName}
              label={feature.label}
              description={feature.description}
              action={
                <Switch
                  checked={rowPending ? !enabled : enabled}
                  // One mutation at a time: `pendingFeature` tracks only the
                  // latest variables, so concurrent toggles would flash stale
                  // optimistic state and can race each other.
                  disabled={isPending}
                  onCheckedChange={(next) =>
                    mutate({
                      request: {
                        setProductFeatureRequestBody: {
                          featureName: feature.featureName,
                          enabled: next,
                        },
                      },
                    })
                  }
                  aria-label={`Toggle ${feature.label}`}
                />
              }
            >
              {rowError && (
                <p className="text-destructive mt-2 text-sm">{rowError}</p>
              )}
            </AdminRow>
          );
        })}
      </div>
    );
  };

  return (
    <AdminSection
      title="Product features"
      description="Org-level entitlements. Changes apply immediately to every member of the organization."
    >
      {body()}
    </AdminSection>
  );
}

const WORK_UNITS_MAX_CAP = 10_000;
// Prefilled when enabling an organization whose stored cap is 0 — a cap of 0
// disables scoring as surely as the switch.
const WORK_UNITS_SUGGESTED_CAP = 100;

function RunChatAnalysisNow(): JSX.Element {
  const trigger = useTriggerChatAnalysisMutation({
    onSuccess: (data) => {
      toast.success(
        `Chat analysis triggered for ${data.projectsSignaled} project${data.projectsSignaled === 1 ? "" : "s"}.`,
      );
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Failed to trigger chat analysis",
      );
    },
  });

  return (
    <div className="mt-3 flex items-center justify-between gap-4">
      <p className="text-muted-foreground text-sm">
        Wake every project's analysis coordinator now instead of waiting for the
        sweep.
      </p>
      <Button
        variant="secondary"
        size="sm"
        onClick={() => trigger.mutate({})}
        disabled={trigger.isPending}
      >
        {trigger.isPending ? "Running…" : "Run now"}
      </Button>
    </div>
  );
}

// The chat analysis pipelines are not product features: they write the
// chat_analysis_settings row (adminChatAnalysis service) that the analysis
// reservation spends against, so they get their own section beside the
// entitlement toggles rather than rows among them.
function ChatAnalysisSection(): JSX.Element {
  return (
    <AdminSection
      title="Chat analysis"
      description="Caps are evaluations per UTC day; a cap of 0 disables the pipeline."
    >
      <div className="divide-border divide-y">
        <WorkUnitsAnalysisRow />
        <BusinessMemoryAnalysisRow />
      </div>
    </AdminSection>
  );
}

function AnalysisRow({
  label,
  description,
  capAriaLabel,
  enabled,
  storedCap,
  isLoading,
  loadError,
  mutationPending,
  mutationError,
  onUpsert,
}: {
  label: string;
  description: string;
  capAriaLabel: string;
  enabled: boolean;
  storedCap: number;
  isLoading: boolean;
  loadError: string | null;
  mutationPending: boolean;
  mutationError: string | undefined;
  onUpsert: (enabled: boolean, dailyCap: number, onDone: () => void) => void;
}): JSX.Element {
  // undefined mirrors the stored cap; a string is a local edit in progress.
  const [capInput, setCapInput] = useState<string>();

  if (isLoading) {
    return <AdminRow label={label} description={<span>Loading…</span>} />;
  }
  if (loadError !== null) {
    return (
      <AdminRow label={label}>
        <p className="text-destructive mt-1 text-sm">{loadError}</p>
      </AdminRow>
    );
  }

  const cap = capInput ?? String(storedCap);
  const capNumber = Number(cap);
  const capValid =
    cap.trim() !== "" &&
    Number.isInteger(capNumber) &&
    capNumber >= 0 &&
    capNumber <= WORK_UNITS_MAX_CAP;
  const capDirty = capValid && capNumber !== storedCap;

  const upsert = (nextEnabled: boolean, dailyCap: number) => {
    onUpsert(nextEnabled, dailyCap, () => setCapInput(undefined));
  };

  // One contextual action: enable when off, save an edited cap, disable
  // otherwise.
  const action = () => {
    if (!enabled) {
      // A cap of 0 disables the pipeline as surely as the switch, so an
      // enable with 0 commits the suggested cap instead — reflect that in
      // the input immediately so the field never contradicts the submit.
      if (capNumber === 0) {
        setCapInput(String(WORK_UNITS_SUGGESTED_CAP));
      }
      upsert(true, capNumber > 0 ? capNumber : WORK_UNITS_SUGGESTED_CAP);
    } else if (capDirty) {
      upsert(true, capNumber);
    } else {
      upsert(false, capNumber);
    }
  };
  const actionLabel = () => {
    if (!enabled) return "Enable";
    if (capDirty) return "Save cap";
    return "Disable";
  };

  return (
    <AdminRow
      label={
        <span className="flex items-center gap-2">
          {label}
          <Badge variant={enabled ? "success" : "neutral"} size="sm">
            {enabled ? "Enabled" : "Disabled"}
          </Badge>
        </span>
      }
      description={description}
      action={
        <div className="flex items-center gap-2">
          <Input
            value={cap}
            onChange={setCapInput}
            aria-label={capAriaLabel}
            className="w-24"
          />
          <Button
            variant={enabled && !capDirty ? "destructive-primary" : "primary"}
            size="sm"
            onClick={action}
            disabled={!capValid || mutationPending}
          >
            {actionLabel()}
          </Button>
        </div>
      }
    >
      {/* A zero cap disables the judge server-side, so "Run now" would no-op. */}
      {enabled && storedCap > 0 && <RunChatAnalysisNow />}
      {!capValid && (
        <p className="text-destructive mt-2 text-sm">
          Cap must be a whole number from 0 to{" "}
          {WORK_UNITS_MAX_CAP.toLocaleString()}.
        </p>
      )}
      {mutationError && (
        <p className="text-destructive mt-2 text-sm">{mutationError}</p>
      )}
    </AdminRow>
  );
}

function WorkUnitsAnalysisRow(): JSX.Element {
  const queryClient = useQueryClient();
  const query = useChatAnalysisSettings(undefined, undefined, {
    throwOnError: false,
  });
  const mutation = useUpsertChatAnalysisSettingsMutation();

  return (
    <AnalysisRow
      label="Work Delivered Chat Analysis"
      description="Evaluates work delivered in the organization's quiet chat sessions."
      capAriaLabel="Work delivered daily evaluation cap"
      enabled={query.data?.workUnitsEnabled ?? false}
      storedCap={query.data?.workUnitsDailyCap ?? 0}
      isLoading={query.isLoading}
      loadError={
        query.isLoading || query.data
          ? null
          : `Failed to load chat analysis settings: ${query.error?.message ?? "unknown error"}`
      }
      mutationPending={mutation.isPending}
      mutationError={mutation.error?.message}
      onUpsert={(enabled, dailyCap, onDone) =>
        mutation.mutate(
          {
            request: {
              upsertWorkUnitsSettingsRequestBody: {
                workUnitsEnabled: enabled,
                workUnitsDailyCap: dailyCap,
              },
            },
          },
          {
            onSuccess: () => {
              onDone();
              void invalidateAllChatAnalysisSettings(queryClient);
            },
          },
        )
      }
    />
  );
}

function BusinessMemoryAnalysisRow(): JSX.Element {
  const queryClient = useQueryClient();
  const query = useChatAnalysisSettings(undefined, undefined, {
    throwOnError: false,
  });
  const mutation = useUpsertBusinessMemoryAnalysisSettingsMutation();

  return (
    <AnalysisRow
      label="Business Memory Extraction"
      description="Extracts reusable glossary entries, procedures, and prior results from quiet chat sessions."
      capAriaLabel="Business memory daily extraction cap"
      enabled={query.data?.businessMemoryEnabled ?? false}
      storedCap={query.data?.businessMemoryDailyCap ?? 0}
      isLoading={query.isLoading}
      loadError={
        query.isLoading || query.data
          ? null
          : `Failed to load business memory settings: ${query.error?.message ?? "unknown error"}`
      }
      mutationPending={mutation.isPending}
      mutationError={mutation.error?.message}
      onUpsert={(enabled, dailyCap, onDone) =>
        mutation.mutate(
          {
            request: {
              upsertBusinessMemorySettingsRequestBody: {
                businessMemoryEnabled: enabled,
                businessMemoryDailyCap: dailyCap,
              },
            },
          },
          {
            onSuccess: () => {
              onDone();
              void invalidateAllChatAnalysisSettings(queryClient);
            },
          },
        )
      }
    />
  );
}

function DeveloperSection(): JSX.Element {
  const [orgMemoryEnabled, setOrgMemoryEnabled] = useOrgMemoryDeveloperToggle();

  return (
    <AdminSection
      title="Developer"
      description="Local-only toggles for this browser session."
    >
      <AdminRow
        label="Org Memory dashboard"
        description="Visible for this browser session"
        action={
          <Switch
            checked={orgMemoryEnabled}
            onCheckedChange={setOrgMemoryEnabled}
            aria-label="Toggle Org Memory dashboard"
          />
        }
      />
    </AdminSection>
  );
}
