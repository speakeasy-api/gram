import { Page } from "@/components/page-layout";
import {
  PageTabsTrigger,
  Tabs,
  TabsContent,
  TabsList,
} from "@/components/ui/tabs";
import { Dialog } from "@/components/ui/dialog";
import { Heading } from "@/components/ui/heading";
import { Input } from "@/components/ui/input";
import { Type } from "@/components/ui/type";
import { useIsAdmin, useOrganization } from "@/contexts/Auth";
import { useSdkClient } from "@/contexts/Sdk";
import { FeatureName } from "@gram/client/models/components";
import {
  useDisableRBACMutation,
  useEnableRBACMutation,
  useFeaturesSetMutation,
  useProductFeatures,
  useRbacStatus,
} from "@gram/client/react-query";
import { invalidateAllProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { invalidateAllRbacStatus } from "@gram/client/react-query/rbacStatus.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRightLeft,
  Building2,
  FileSearch,
  FolderSync,
  KeyRound,
  Loader2,
  ShieldCheck,
  Webhook,
} from "lucide-react";
import { useState } from "react";

function FeatureToggle({
  label,
  description,
  icon: Icon,
  featureName,
  enabled,
  isPending,
  onToggle,
  error,
}: {
  label: string;
  description: string;
  icon: React.ComponentType<{ className?: string }>;
  featureName: FeatureName;
  enabled: boolean;
  isPending: boolean;
  onToggle: (featureName: FeatureName, enabled: boolean) => void;
  error?: string;
}) {
  return (
    <div className="border-border bg-card rounded-lg border p-4">
      <Stack direction="horizontal" align="center" gap={2} className="mb-1">
        <Icon className="text-muted-foreground h-4 w-4" />
        <Type variant="body" className="font-medium">
          {label}
        </Type>
      </Stack>
      <Type muted small className="mb-4 ml-6">
        {description}
      </Type>
      <div className="ml-6 space-y-3">
        <div className="flex items-center gap-3">
          <Type variant="body" className="font-medium">
            Status:
          </Type>
          {enabled ? (
            <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-500">
              Enabled
            </span>
          ) : (
            <span className="bg-muted text-muted-foreground inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium">
              Disabled
            </span>
          )}
        </div>

        <div className="flex gap-2">
          {enabled ? (
            <Button
              variant="destructive-primary"
              onClick={() => onToggle(featureName, false)}
              disabled={isPending}
            >
              {isPending && (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Disable</Button.Text>
            </Button>
          ) : (
            <Button
              onClick={() => onToggle(featureName, true)}
              disabled={isPending}
            >
              {isPending && (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Enable</Button.Text>
            </Button>
          )}
        </div>

        {error && <Type className="text-destructive text-sm">{error}</Type>}
      </div>
    </div>
  );
}

function RBACManagementSection() {
  const queryClient = useQueryClient();
  const [confirmAction, setConfirmAction] = useState<
    "enable" | "disable" | null
  >(null);

  const { data: status, isLoading, error } = useRbacStatus();

  const enableMutation = useEnableRBACMutation({
    onSuccess: () => {
      invalidateAllRbacStatus(queryClient);
      setConfirmAction(null);
    },
  });

  const disableMutation = useDisableRBACMutation({
    onSuccess: () => {
      invalidateAllRbacStatus(queryClient);
      setConfirmAction(null);
    },
  });

  const toggleMutation =
    confirmAction === "enable" ? enableMutation : disableMutation;

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-4">
        <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
        <Type muted small>
          Loading...
        </Type>
      </div>
    );
  }

  if (error || !status) {
    return (
      <Type className="text-destructive py-2 text-sm">
        Failed to load RBAC status: {error?.message ?? "unknown error"}
      </Type>
    );
  }

  return (
    <>
      <div className="space-y-4">
        <div className="flex items-center gap-3">
          <Type variant="body" className="font-medium">
            Status:
          </Type>
          {status.rbacEnabled ? (
            <span className="inline-flex items-center rounded-full bg-emerald-500/10 px-2.5 py-0.5 text-xs font-medium text-emerald-500">
              Enabled
            </span>
          ) : (
            <span className="bg-muted text-muted-foreground inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium">
              Disabled
            </span>
          )}
        </div>

        <div className="flex gap-2 pt-2">
          {status.rbacEnabled ? (
            <Button
              variant="destructive-primary"
              onClick={() => setConfirmAction("disable")}
              disabled={toggleMutation.isPending}
            >
              {toggleMutation.isPending && (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Disable RBAC</Button.Text>
            </Button>
          ) : (
            <Button
              onClick={() => setConfirmAction("enable")}
              disabled={toggleMutation.isPending}
            >
              {toggleMutation.isPending && (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>Enable RBAC</Button.Text>
            </Button>
          )}
        </div>

        {toggleMutation.error && (
          <Type className="text-destructive text-sm">
            {toggleMutation.error.message}
          </Type>
        )}
      </div>

      <Dialog
        open={confirmAction !== null}
        onOpenChange={(open) => {
          if (!open) setConfirmAction(null);
        }}
      >
        <Dialog.Content className="sm:max-w-md">
          <Dialog.Header>
            <Dialog.Title>
              {confirmAction === "enable" ? "Enable RBAC" : "Disable RBAC"}
            </Dialog.Title>
            <Dialog.Description>
              {confirmAction === "enable"
                ? "This will seed default grants for system roles and enforce access control for this organization."
                : "This will disable access control enforcement. All members will have unrestricted access."}
            </Dialog.Description>
          </Dialog.Header>
          <div className="flex justify-end gap-2 pt-4">
            <Button variant="secondary" onClick={() => setConfirmAction(null)}>
              Cancel
            </Button>
            <Button
              variant={
                confirmAction === "disable" ? "destructive-primary" : undefined
              }
              onClick={() => toggleMutation.mutate({})}
              disabled={toggleMutation.isPending}
            >
              {toggleMutation.isPending && (
                <Button.LeftIcon>
                  <Loader2 className="h-4 w-4 animate-spin" />
                </Button.LeftIcon>
              )}
              <Button.Text>
                {confirmAction === "enable" ? "Enable" : "Disable"}
              </Button.Text>
            </Button>
          </div>
        </Dialog.Content>
      </Dialog>
    </>
  );
}

function ProductFeaturesTab() {
  const queryClient = useQueryClient();
  const { data: features, isLoading, error } = useProductFeatures();

  const {
    mutate,
    isPending,
    error: mutError,
    variables,
  } = useFeaturesSetMutation({
    onSuccess: () => {
      invalidateAllProductFeatures(queryClient);
    },
  });

  if (isLoading) {
    return (
      <div className="flex items-center gap-2 py-4">
        <Loader2 className="text-muted-foreground h-4 w-4 animate-spin" />
        <Type muted small>
          Loading...
        </Type>
      </div>
    );
  }

  if (error || !features) {
    return (
      <Type className="text-destructive py-2 text-sm">
        Failed to load feature flags: {error?.message ?? "unknown error"}
      </Type>
    );
  }

  const pendingFeature =
    variables?.request?.setProductFeatureRequestBody?.featureName;

  const handleToggle = (featureName: FeatureName, enabled: boolean) => {
    mutate({
      request: {
        setProductFeatureRequestBody: { featureName, enabled },
      },
    });
  };

  return (
    <div className="space-y-4">
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack direction="horizontal" align="center" gap={2} className="mb-1">
          <ShieldCheck className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            RBAC
          </Type>
        </Stack>
        <Type muted small className="mb-4 ml-6">
          Role-based access control enforcement. Ensure all members have roles
          assigned before enabling.
        </Type>
        <div className="ml-6">
          <RBACManagementSection />
        </div>
      </div>

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

export default function OrgAdminSettings() {
  const isAdmin = useIsAdmin();

  if (!isAdmin) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Type muted>You do not have access to this page.</Type>
        </Page.Body>
      </Page>
    );
  }

  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        <OrgAdminSettingsInner />
      </Page.Body>
    </Page>
  );
}

export function OrgAdminSettingsInner() {
  const organization = useOrganization();
  const client = useSdkClient();

  return (
    <Tabs defaultValue="info">
      <div className="border-border -mx-8 border-b px-8">
        <TabsList className="h-auto justify-start gap-4 rounded-none bg-transparent p-0 text-sm">
          <PageTabsTrigger value="info">Info</PageTabsTrigger>
          <PageTabsTrigger value="features">Product Features</PageTabsTrigger>
        </TabsList>
      </div>

      <TabsContent value="info" className="mt-6">
        <Heading variant="h4" className="mb-2">
          Organization Info
        </Heading>
        <Type muted small className="mb-4">
          Details about the current organization.
        </Type>
        <div className="border-border bg-card mb-8 rounded-lg border p-4">
          <Stack direction="horizontal" align="center" gap={2} className="mb-3">
            <Building2 className="text-muted-foreground h-4 w-4" />
            <Type variant="body" className="font-medium">
              Organization
            </Type>
          </Stack>
          <div className="ml-6 space-y-1">
            <div className="flex gap-3">
              <Type variant="body" className="text-muted-foreground text-sm">
                Slug
              </Type>
              <Type variant="body" className="font-mono text-sm">
                {organization.slug}
              </Type>
            </div>
            <div className="flex gap-3">
              <Type variant="body" className="text-muted-foreground text-sm">
                ID
              </Type>
              <Type variant="body" className="font-mono text-sm">
                {organization.id}
              </Type>
            </div>
          </div>
        </div>

        <Heading variant="h4" className="mb-2">
          Organization Override
        </Heading>
        <Type muted small className="mb-4">
          Impersonate a different organization by switching to its slug. This
          will log you out and redirect you to the target organization.
        </Type>
        <div className="border-border bg-card rounded-lg border p-4">
          <Stack direction="horizontal" align="center" gap={2} className="mb-4">
            <ArrowRightLeft className="text-muted-foreground h-4 w-4" />
            <Type variant="body" className="font-medium">
              Switch Organization
            </Type>
          </Stack>
          <form
            onSubmit={async (e) => {
              e.preventDefault();
              const formData = new FormData(e.currentTarget);
              const val = formData.get("gram_admin_override");
              if (typeof val !== "string" || !val.trim()) {
                return;
              }

              await client.auth.logout();
              document.cookie = `gram_admin_override=${val.trim()}; path=/; max-age=31536000;`;
              window.location.href = "/login";
            }}
            className="ml-6 flex max-w-md gap-2"
          >
            <Input
              placeholder="organization-slug"
              name="gram_admin_override"
              className="flex-1"
              required
            />
            <Button type="submit">Go to Org</Button>
            <Button
              variant="secondary"
              type="button"
              onClick={async () => {
                document.cookie = `gram_admin_override=; path=/; max-age=0;`;
                await client.auth.logout();
                window.location.href = "/login";
              }}
            >
              Clear override
            </Button>
          </form>
        </div>
      </TabsContent>

      <TabsContent value="features" className="mt-6">
        <ProductFeaturesTab />
      </TabsContent>
    </Tabs>
  );
}
