import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Card } from "@/components/ui/card";
import { DetailList } from "@/components/ui/detail-list";
import { Type } from "@/components/ui/type";
import { useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { toastError } from "@/lib/toast-error";
import { FeatureName } from "@gram/client/models/components/setproductfeaturerequestbody.js";
import { useDisableRBACMutation } from "@gram/client/react-query/disableRBAC.js";
import { useEnableRBACMutation } from "@gram/client/react-query/enableRBAC.js";
import { useFeaturesSetMutation } from "@gram/client/react-query/featuresSet.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useRbacStatus } from "@gram/client/react-query/rbacStatus.js";
import { useSendEnterpriseAdminOnboardingEmailMutation } from "@gram/client/react-query/sendEnterpriseAdminOnboardingEmail.js";
import { invalidateAllProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { invalidateAllRbacStatus } from "@gram/client/react-query/rbacStatus.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRightLeft,
  Building2,
  FileSearch,
  FolderSync,
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
  return (
    <Badge variant={enabled ? "success" : "neutral"} size="sm">
      {enabled ? "Enabled" : "Disabled"}
    </Badge>
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
    <Button
      type="button"
      variant={destructive ? "destructive-secondary" : "primary"}
      size="xs"
      onClick={onClick}
      disabled={disabled || pending}
    >
      {pending && (
        <Button.LeftIcon>
          <Loader2 className="animate-spin" />
        </Button.LeftIcon>
      )}
      <Button.Text>{children}</Button.Text>
    </Button>
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
    <Card size="sm" className="gap-2 p-3">
      <div className="flex items-center gap-1.5">
        <Icon className="text-muted-foreground h-3.5 w-3.5" />
        <Type small className="font-medium">
          {title}
        </Type>
      </div>
      {description && (
        <Type muted small className="leading-snug">
          {description}
        </Type>
      )}
      {children}
    </Card>
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
      {error && (
        <Type destructive small className="mt-2">
          {error}
        </Type>
      )}
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
      setConfirmAction(null);
    },
  });

  const disableMutation = useDisableRBACMutation({
    onSuccess: () => {
      void invalidateAllRbacStatus(queryClient);
      setConfirmAction(null);
    },
  });

  const toggleMutation =
    confirmAction === "enable" ? enableMutation : disableMutation;

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-1">
        <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
        <Type muted small>
          Loading…
        </Type>
      </div>
    );
  }

  if (error || !status) {
    return (
      <Type destructive small>
        Failed to load RBAC status: {error?.message ?? "unknown error"}
      </Type>
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
        <Card size="sm" className="gap-2 bg-muted/40 p-2">
          <Type small className="leading-snug">
            {confirmAction === "enable"
              ? "Seed default grants for system roles and enforce access control for this organization?"
              : "Disable access control enforcement? All members will have unrestricted access."}
          </Type>
          <div className="flex items-center gap-2">
            <ActionButton
              onClick={() => toggleMutation.mutate({})}
              pending={toggleMutation.isPending}
              destructive={confirmAction === "disable"}
            >
              {confirmAction === "enable" ? "Enable" : "Disable"}
            </ActionButton>
            <Button
              type="button"
              variant="tertiary"
              size="xs"
              onClick={() => setConfirmAction(null)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </div>
        </Card>
      )}

      {toggleMutation.error && (
        <Type destructive small>
          {toggleMutation.error.message}
        </Type>
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
    onSuccess: () => {
      void invalidateAllProductFeatures(queryClient);
    },
  });

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-1">
        <Loader2 className="text-muted-foreground h-3.5 w-3.5 animate-spin" />
        <Type muted small>
          Loading…
        </Type>
      </div>
    );
  }

  if (error || !features) {
    return (
      <Type destructive small>
        Failed to load feature flags: {error?.message ?? "unknown error"}
      </Type>
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
    </div>
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
      toastError(err, "Failed to send onboarding email");
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
          onChange={(e) => setEmailsInput(e.target.value)}
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
          <Type muted small className="pt-1 break-all">
            Setup link:{" "}
            <code className="bg-muted px-1 py-0.5 font-mono text-[10px]">
              {sendEmail.data.setupLink}
            </code>
          </Type>
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
          <Button
            type="button"
            variant="tertiary"
            size="xs"
            onClick={() => {
              void (async () => {
                document.cookie = `gram_admin_override=; path=/; max-age=0;`;
                await client.auth.logout();
                window.location.href = "/login";
              })();
            }}
          >
            <Button.Text>Clear override</Button.Text>
          </Button>
          <Button type="submit" variant="primary" size="xs">
            <Button.Text>Go to org</Button.Text>
          </Button>
        </div>
      </form>
    </Section>
  );
}

function OrgInfoSection(): ReactElement {
  const organization = useOrganization();
  return (
    <Section icon={Building2} title="Organization">
      <DetailList orientation="inline" className="gap-y-1 text-[11px]">
        <DetailList.Item
          label="Slug"
          value={
            <span className="truncate font-mono">{organization.slug}</span>
          }
        />
        <DetailList.Item
          label="ID"
          value={<span className="truncate font-mono">{organization.id}</span>}
        />
      </DetailList>
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
