import { Input } from "@/components/ui/input";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import {
  invalidateAllChatAnalysisSettings,
  useChatAnalysisSettings,
} from "@gram/client/react-query/chatAnalysisSettings.js";
import { useDisableRBACMutation } from "@gram/client/react-query/disableRBAC.js";
import { useEnableRBACMutation } from "@gram/client/react-query/enableRBAC.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import { invalidateAllGrants } from "@gram/client/react-query/grants.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRbacStatus } from "@gram/client/react-query/rbacStatus.js";
import { useSendEnterpriseAdminOnboardingEmailMutation } from "@gram/client/react-query/sendEnterpriseAdminOnboardingEmail.js";
import { useTriggerChatAnalysisMutation } from "@gram/client/react-query/triggerChatAnalysis.js";
import { useUpsertChatAnalysisSettingsMutation } from "@gram/client/react-query/upsertChatAnalysisSettings.js";
import { invalidateAllProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { invalidateAllRbacStatus } from "@gram/client/react-query/rbacStatus.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRightLeft,
  BarChart3,
  BookOpen,
  Building2,
  FileSearch,
  FolderSync,
  Key,
  KeyRound,
  Loader2,
  Mail,
  ShieldCheck,
  Webhook,
} from "lucide-react";
import { ComponentType, ReactElement, useState } from "react";
import { toast } from "sonner";

// These panels surface the Platform Admin tooling (org info & override, product
// features, RBAC, enterprise onboarding) inside the Developer Toolkit, one panel
// per toolkit tab. They replace the standalone OrgAdminSettings page; the layout
// is compact so it fits the narrow platform-admin-toolbar panel. Every control
// here hits a platform-admin guarded endpoint, so a non-platform-admin caller
// sees graceful error states.

function StatusPill({ enabled }: { enabled: boolean }): ReactElement {
  return enabled ? (
    <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2 py-0.5 text-[10px] font-medium text-emerald-500">
      Enabled
    </span>
  ) : (
    <span className="bg-muted text-muted-foreground inline-flex items-center rounded-full px-2 py-0.5 text-[10px] font-medium">
      Disabled
    </span>
  );
}

function ActionButton({
  children,
  onClick,
  disabled,
  pending,
  destructive,
}: {
  children: React.ReactNode;
  onClick: () => void;
  disabled?: boolean;
  pending?: boolean;
  destructive?: boolean;
}): ReactElement {
  return (
    <button
      type="button"
      onClick={onClick}
      disabled={disabled || pending}
      className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-[11px] font-medium transition-colors disabled:opacity-50 ${
        destructive
          ? "bg-red-500/10 text-red-600 hover:bg-red-500/20 dark:text-red-400"
          : "bg-foreground text-background hover:opacity-90"
      }`}
    >
      {pending && <Loader2 className="h-3 w-3 animate-spin" />}
      {children}
    </button>
  );
}

function Section({
  icon: Icon,
  title,
  description,
  children,
}: {
  icon: ComponentType<{ className?: string }>;
  title: string;
  description?: string;
  children: React.ReactNode;
}): ReactElement {
  return (
    <div className="border-border bg-card rounded-lg border p-3">
      <div className="mb-1 flex items-center gap-1.5">
        <Icon className="text-muted-foreground h-3.5 w-3.5" />
        <span className="text-foreground text-xs font-medium">{title}</span>
      </div>
      {description && (
        <p className="text-muted-foreground mb-2.5 text-[11px] leading-snug">
          {description}
        </p>
      )}
      {children}
    </div>
  );
}

function FeatureToggle({
  label,
  description,
  icon,
  featureName,
  enabled,
  isPending,
  onToggle,
  error,
}: {
  label: string;
  description: string;
  icon: ComponentType<{ className?: string }>;
  featureName: FeatureName;
  enabled: boolean;
  isPending: boolean;
  onToggle: (featureName: FeatureName, enabled: boolean) => void;
  error?: string;
}): ReactElement {
  return (
    <Section icon={icon} title={label} description={description}>
      <div className="flex items-center justify-between gap-2">
        <StatusPill enabled={enabled} />
        <ActionButton
          onClick={() => onToggle(featureName, !enabled)}
          pending={isPending}
          destructive={enabled}
        >
          {enabled ? "Disable" : "Enable"}
        </ActionButton>
      </div>
      {error && <p className="text-destructive mt-2 text-[11px]">{error}</p>}
    </Section>
  );
}

function RBACManagementSection(): ReactElement {
  const queryClient = useQueryClient();
  const [confirmAction, setConfirmAction] = useState<
    "enable" | "disable" | null
  >(null);

  const { data: status, isLoading, error } = useRbacStatus();

  const enableMutation = useEnableRBACMutation({
    onSuccess: () => {
      void invalidateAllRbacStatus(queryClient);
      void invalidateAllGrants(queryClient);
      setConfirmAction(null);
    },
  });

  const disableMutation = useDisableRBACMutation({
    onSuccess: () => {
      void invalidateAllRbacStatus(queryClient);
      void invalidateAllGrants(queryClient);
      setConfirmAction(null);
    },
  });

  const toggleMutation =
    confirmAction === "enable" ? enableMutation : disableMutation;

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-1">
        <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
        <span className="text-muted-foreground text-[11px]">Loading…</span>
      </div>
    );
  }

  if (error || !status) {
    return (
      <p className="text-destructive text-[11px]">
        Failed to load RBAC status: {error?.message ?? "unknown error"}
      </p>
    );
  }

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <StatusPill enabled={status.rbacEnabled} />
        {confirmAction === null && (
          <ActionButton
            onClick={() =>
              setConfirmAction(status.rbacEnabled ? "disable" : "enable")
            }
            destructive={status.rbacEnabled}
          >
            {status.rbacEnabled ? "Disable RBAC" : "Enable RBAC"}
          </ActionButton>
        )}
      </div>

      {confirmAction !== null && (
        // Inline confirmation instead of a modal: the platform-admin-toolbar collapses on
        // outside clicks, which would tear down a portalled dialog mid-flow.
        <div className="border-border bg-muted/40 rounded-md border p-2">
          <p className="text-foreground mb-2 text-[11px] leading-snug">
            {confirmAction === "enable"
              ? "Seed default grants for system roles and enforce access control for this organization?"
              : "Disable access control enforcement? All members will have unrestricted access."}
          </p>
          <div className="flex items-center gap-2">
            <ActionButton
              onClick={() => toggleMutation.mutate({})}
              pending={toggleMutation.isPending}
              destructive={confirmAction === "disable"}
            >
              {confirmAction === "enable" ? "Enable" : "Disable"}
            </ActionButton>
            <button
              type="button"
              onClick={() => setConfirmAction(null)}
              className="text-muted-foreground hover:text-foreground text-[11px]"
            >
              Cancel
            </button>
          </div>
        </div>
      )}

      {toggleMutation.error && (
        <p className="text-destructive text-[11px]">
          {toggleMutation.error.message}
        </p>
      )}
    </div>
  );
}

function ProductFeaturesSection(): ReactElement {
  const queryClient = useQueryClient();
  const { data: features, isLoading, error } = useProductFeatures();

  const {
    mutate,
    isPending,
    error: mutError,
    variables,
  } = useFeaturesSetMutation({
    onSuccess: (_data, mutationVariables) => {
      void invalidateAllProductFeatures(queryClient);
      if (
        mutationVariables.request?.setProductFeatureRequestBody?.featureName ===
        FeatureName.Skills
      ) {
        void invalidateAllGrants(queryClient);
      }
    },
  });

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-1">
        <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
        <span className="text-muted-foreground text-[11px]">Loading…</span>
      </div>
    );
  }

  if (error || !features) {
    return (
      <p className="text-destructive text-[11px]">
        Failed to load feature flags: {error?.message ?? "unknown error"}
      </p>
    );
  }

  const pendingFeature =
    variables?.request?.setProductFeatureRequestBody?.featureName;

  const handleToggle = (featureName: FeatureName, enabled: boolean) => {
    mutate({
      request: { setProductFeatureRequestBody: { featureName, enabled } },
    });
  };

  return (
    <div className="space-y-2">
      <Section
        icon={ShieldCheck}
        title="RBAC"
        description="Role-based access control enforcement. Ensure all members have roles assigned before enabling."
      >
        <RBACManagementSection />
      </Section>

      <FeatureToggle
        label="Skills"
        description="Enables the Skills page and provisions default Skills grants when RBAC is active."
        icon={BookOpen}
        featureName={FeatureName.Skills}
        enabled={features.skillsEnabled}
        isPending={isPending && pendingFeature === FeatureName.Skills}
        onToggle={handleToggle}
        error={
          pendingFeature === FeatureName.Skills ? mutError?.message : undefined
        }
      />

      <FeatureToggle
        label="Authz Challenge Logging"
        description='Log every authorization decision (allow/deny) to ClickHouse. Powers auditing of "why did X have access to Y?"'
        icon={FileSearch}
        featureName={FeatureName.AuthzChallengeLogging}
        enabled={features.authzChallengeLoggingEnabled}
        isPending={
          isPending && pendingFeature === FeatureName.AuthzChallengeLogging
        }
        onToggle={handleToggle}
        error={
          pendingFeature === FeatureName.AuthzChallengeLogging
            ? mutError?.message
            : undefined
        }
      />

      <FeatureToggle
        label="Webhooks"
        description="Unlocks the Webhooks page for this organization (Svix-backed event delivery). While disabled, members see the preview gate."
        icon={Webhook}
        featureName={FeatureName.Webhooks}
        enabled={features.webhooks}
        isPending={isPending && pendingFeature === FeatureName.Webhooks}
        onToggle={handleToggle}
        error={
          pendingFeature === FeatureName.Webhooks
            ? mutError?.message
            : undefined
        }
      />

      <FeatureToggle
        label="Custom Model Provider Keys"
        description="Allows projects in this organization to store OpenRouter API keys for model completions."
        icon={Key}
        featureName={FeatureName.CustomModelKeys}
        enabled={features.customModelKeysEnabled}
        isPending={isPending && pendingFeature === FeatureName.CustomModelKeys}
        onToggle={handleToggle}
        error={
          pendingFeature === FeatureName.CustomModelKeys
            ? mutError?.message
            : undefined
        }
      />

      <FeatureToggle
        label="SSO"
        description="Enables WorkOS portal link creation for managing SSO."
        icon={KeyRound}
        featureName={FeatureName.Sso}
        enabled={features.ssoEnabled}
        isPending={isPending && pendingFeature === FeatureName.Sso}
        onToggle={handleToggle}
        error={
          pendingFeature === FeatureName.Sso ? mutError?.message : undefined
        }
      />

      <FeatureToggle
        label="SCIM"
        description="Enables WorkOS portal link creation for managing SCIM."
        icon={FolderSync}
        featureName={FeatureName.Scim}
        enabled={features.scimEnabled}
        isPending={isPending && pendingFeature === FeatureName.Scim}
        onToggle={handleToggle}
        error={
          pendingFeature === FeatureName.Scim ? mutError?.message : undefined
        }
      />

      <WorkUnitsAnalysisSection />
    </div>
  );
}

const WORK_UNITS_MAX_CAP = 10_000;
// Prefilled when enabling an organization whose stored cap is 0 — a cap of 0
// disables scoring as surely as the switch.
const WORK_UNITS_SUGGESTED_CAP = 100;

// WorkUnitsAnalysisSection controls the chat analysis pipeline's work-units
// judge for the organization. Not a product feature: it writes the
// chat_analysis_settings row (adminChatAnalysis service) that the analysis
// reservation spends against, so it lives beside the feature toggles rather
// than among them.
function WorkUnitsAnalysisSection(): ReactElement {
  const queryClient = useQueryClient();
  const query = useChatAnalysisSettings(undefined, undefined, {
    throwOnError: false,
  });
  // undefined mirrors the stored cap; a string is a local edit in progress.
  const [capInput, setCapInput] = useState<string>();

  const mutation = useUpsertChatAnalysisSettingsMutation({
    onSuccess: async () => {
      setCapInput(undefined);
      await invalidateAllChatAnalysisSettings(queryClient);
    },
  });

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

  const section = (children: React.ReactNode) => (
    <Section
      icon={BarChart3}
      title="Work Units Chat Analysis"
      description="Runs the work-units judge over the organization's quiet chat sessions. Cap is evaluations per UTC day; 0 disables scoring."
    >
      {children}
    </Section>
  );

  if (query.isLoading) {
    return section(
      <div className="flex items-center gap-2 py-1">
        <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
        <span className="text-muted-foreground text-[11px]">Loading…</span>
      </div>,
    );
  }
  if (query.error || !query.data) {
    return section(
      <p className="text-destructive text-[11px]">
        Failed to load chat analysis settings:{" "}
        {query.error?.message ?? "unknown error"}
      </p>,
    );
  }

  const settings = query.data;
  const cap = capInput ?? String(settings.workUnitsDailyCap);
  const capNumber = Number(cap);
  const capValid =
    cap.trim() !== "" &&
    Number.isInteger(capNumber) &&
    capNumber >= 0 &&
    capNumber <= WORK_UNITS_MAX_CAP;
  const capDirty = capValid && capNumber !== settings.workUnitsDailyCap;

  const upsert = (enabled: boolean, dailyCap: number) => {
    mutation.mutate({
      request: {
        upsertWorkUnitsSettingsRequestBody: {
          workUnitsEnabled: enabled,
          workUnitsDailyCap: dailyCap,
        },
      },
    });
  };

  // One contextual action: enable when off, save an edited cap, disable
  // otherwise.
  const action = () => {
    if (!settings.workUnitsEnabled) {
      upsert(true, capNumber > 0 ? capNumber : WORK_UNITS_SUGGESTED_CAP);
    } else if (capDirty) {
      upsert(true, capNumber);
    } else {
      upsert(false, capNumber);
    }
  };
  const actionLabel = () => {
    if (!settings.workUnitsEnabled) return "Enable";
    if (capDirty) return "Save cap";
    return "Disable";
  };

  return section(
    <>
      <div className="flex items-center justify-between gap-2">
        <StatusPill enabled={settings.workUnitsEnabled} />
        <div className="flex items-center gap-1.5">
          <Input
            value={cap}
            onChange={setCapInput}
            aria-label="Work-units daily evaluation cap"
            className="h-6 w-20 px-2 text-[11px]"
          />
          <ActionButton
            onClick={action}
            pending={mutation.isPending}
            disabled={!capValid}
            destructive={settings.workUnitsEnabled && !capDirty}
          >
            {actionLabel()}
          </ActionButton>
        </div>
      </div>
      {settings.workUnitsEnabled && (
        <div className="mt-2 flex items-center justify-between gap-2">
          <p className="text-muted-foreground text-[11px]">
            Wake every project's analysis coordinator now instead of waiting for
            the sweep.
          </p>
          <ActionButton
            onClick={() => trigger.mutate({})}
            pending={trigger.isPending}
          >
            Run now
          </ActionButton>
        </div>
      )}
      {!capValid && (
        <p className="text-destructive mt-2 text-[11px]">
          Cap must be a whole number from 0 to{" "}
          {WORK_UNITS_MAX_CAP.toLocaleString()}.
        </p>
      )}
      {mutation.error && (
        <p className="text-destructive mt-2 text-[11px]">
          {mutation.error.message}
        </p>
      )}
    </>,
  );
}

function OnboardingSection(): ReactElement {
  const [emailsInput, setEmailsInput] = useState("");

  const sendEmail = useSendEnterpriseAdminOnboardingEmailMutation({
    onSuccess: (data) => {
      toast.success(
        `Sent ${data.sentCount} email${data.sentCount === 1 ? "" : "s"}.`,
      );
      setEmailsInput("");
    },
    onError: (err) => {
      toast.error(
        err instanceof Error ? err.message : "Failed to send onboarding email",
      );
    },
  });

  const recipients = emailsInput
    .split(",")
    .map((e) => e.trim())
    .filter((e) => e.length > 0);

  const handleSend = () => {
    if (recipients.length === 0) return;
    sendEmail.mutate({
      request: {
        sendEnterpriseAdminOnboardingEmailRequestBody: { recipients },
      },
    });
  };

  return (
    <Section
      icon={Mail}
      title="Enterprise admin onboarding"
      description="Send the setup-wizard link to people you want to onboard as enterprise admins of this organization."
    >
      <div className="flex flex-col gap-y-2">
        <Input
          name="onboarding_emails"
          placeholder="alice@example.com, bob@example.com"
          value={emailsInput}
          onChange={setEmailsInput}
          disabled={sendEmail.isPending}
        />
        <div className="flex items-center justify-end">
          <ActionButton
            onClick={handleSend}
            disabled={recipients.length === 0}
            pending={sendEmail.isPending}
          >
            Send to{" "}
            {recipients.length === 0
              ? "0 recipients"
              : `${recipients.length} recipient${recipients.length === 1 ? "" : "s"}`}
          </ActionButton>
        </div>

        {sendEmail.data?.setupLink && (
          <p className="text-muted-foreground pt-1 text-[11px] break-all">
            Setup link:{" "}
            <code className="bg-muted rounded px-1 py-0.5 font-mono text-[10px]">
              {sendEmail.data.setupLink}
            </code>
          </p>
        )}
      </div>
    </Section>
  );
}

function OrgOverrideSection(): ReactElement {
  const client = useSdkClient();

  return (
    <Section
      icon={ArrowRightLeft}
      title="Organization override"
      description="Impersonate a different organization by switching to its slug. This logs you out and redirects you to the target organization."
    >
      <form
        onSubmit={(e) => {
          void (async (e) => {
            e.preventDefault();
            const formData = new FormData(e.currentTarget);
            const val = formData.get("gram_admin_override");
            if (typeof val !== "string" || !val.trim()) {
              return;
            }

            await client.auth.logout();
            document.cookie = `gram_admin_override=${val.trim()}; path=/; max-age=31536000;`;
            window.location.href = "/login";
          })(e);
        }}
        className="flex flex-col gap-y-2"
      >
        <Input
          placeholder="organization-slug"
          name="gram_admin_override"
          required
        />
        <div className="flex items-center justify-end gap-2">
          <button
            type="button"
            onClick={() => {
              void (async () => {
                document.cookie = `gram_admin_override=; path=/; max-age=0;`;
                await client.auth.logout();
                window.location.href = "/login";
              })();
            }}
            className="text-muted-foreground hover:text-foreground text-[11px]"
          >
            Clear override
          </button>
          <button
            type="submit"
            className="bg-foreground text-background inline-flex items-center rounded-md px-2.5 py-1 text-[11px] font-medium hover:opacity-90"
          >
            Go to org
          </button>
        </div>
      </form>
    </Section>
  );
}

function OrgInfoSection(): ReactElement {
  const organization = useOrganization();
  return (
    <Section icon={Building2} title="Organization">
      <dl className="grid grid-cols-[auto_1fr] gap-x-3 gap-y-1 text-[11px]">
        <dt className="text-muted-foreground">Slug</dt>
        <dd className="text-foreground truncate font-mono">
          {organization.slug}
        </dd>
        <dt className="text-muted-foreground">ID</dt>
        <dd className="text-foreground truncate font-mono">
          {organization.id}
        </dd>
      </dl>
    </Section>
  );
}

// The panels below back the Platform Admin tabs in the Developer Toolkit, one
// per tab: Info (org info + override), Features (RBAC + product features), and
// Onboarding (enterprise admin email).

export function PlatformAdminInfoPanel(): ReactElement {
  return (
    <div className="space-y-2">
      <OrgInfoSection />
      <OrgOverrideSection />
    </div>
  );
}

export function PlatformAdminFeaturesPanel(): ReactElement {
  return <ProductFeaturesSection />;
}

export function PlatformAdminOnboardingPanel(): ReactElement {
  return <OnboardingSection />;
}
