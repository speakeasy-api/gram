import { Page } from "@/components/page-layout";
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
  useFeaturesGet,
  useFeaturesSetMutation,
  useRbacStatus,
} from "@gram/client/react-query";
import { invalidateAllFeaturesGet } from "@gram/client/react-query/featuresGet.js";
import { invalidateAllRbacStatus } from "@gram/client/react-query/rbacStatus.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import {
  ArrowRightLeft,
  Building2,
  FileSearch,
  Loader2,
  ShieldCheck,
} from "lucide-react";
import { useState } from "react";

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

function AuthzChallengeLoggingSection() {
  const queryClient = useQueryClient();
  const { data: features, isLoading, error } = useFeaturesGet();

  const {
    mutate,
    isPending,
    error: mutError,
  } = useFeaturesSetMutation({
    onSuccess: () => {
      invalidateAllFeaturesGet(queryClient);
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
        Failed to load feature status: {error?.message ?? "unknown error"}
      </Type>
    );
  }

  const enabled = features.authzChallengeLoggingEnabled;

  return (
    <div className="space-y-4">
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

      <div className="flex gap-2 pt-2">
        {enabled ? (
          <Button
            variant="destructive-primary"
            onClick={() =>
              mutate({
                request: {
                  setProductFeatureRequestBody: {
                    featureName: FeatureName.AuthzChallengeLogging,
                    enabled: false,
                  },
                },
              })
            }
            disabled={isPending}
          >
            {isPending && (
              <Button.LeftIcon>
                <Loader2 className="h-4 w-4 animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>Disable Challenge Logging</Button.Text>
          </Button>
        ) : (
          <Button
            onClick={() =>
              mutate({
                request: {
                  setProductFeatureRequestBody: {
                    featureName: FeatureName.AuthzChallengeLogging,
                    enabled: true,
                  },
                },
              })
            }
            disabled={isPending}
          >
            {isPending && (
              <Button.LeftIcon>
                <Loader2 className="h-4 w-4 animate-spin" />
              </Button.LeftIcon>
            )}
            <Button.Text>Enable Challenge Logging</Button.Text>
          </Button>
        )}
      </div>

      {mutError && (
        <Type className="text-destructive text-sm">{mutError.message}</Type>
      )}
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
    <>
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
        Impersonate a different organization by switching to its slug. This will
        log you out and redirect you to the target organization.
      </Type>
      <div className="border-border bg-card mb-8 rounded-lg border p-4">
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

            document.cookie = `gram_admin_override=${val.trim()}; path=/; max-age=31536000;`;
            await client.auth.logout();
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

      <Heading variant="h4" className="mb-2">
        RBAC Management
      </Heading>
      <Type muted small className="mb-4">
        Manage role-based access control for this organization. Ensure all
        members have roles assigned before enabling.
      </Type>
      <div className="border-border bg-card mb-8 rounded-lg border p-4">
        <Stack direction="horizontal" align="center" gap={2} className="mb-4">
          <ShieldCheck className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            Access Control
          </Type>
        </Stack>
        <div className="ml-6">
          <RBACManagementSection />
        </div>
      </div>

      <Heading variant="h4" className="mb-2">
        Authz Challenge Logging
      </Heading>
      <Type muted small className="mb-4">
        Log every authorization decision (allow/deny) to ClickHouse. Powers the
        challenge UI for auditing &ldquo;why did X have access to Y?&rdquo;
      </Type>
      <div className="border-border bg-card rounded-lg border p-4">
        <Stack direction="horizontal" align="center" gap={2} className="mb-4">
          <FileSearch className="text-muted-foreground h-4 w-4" />
          <Type variant="body" className="font-medium">
            Challenge Logging
          </Type>
        </Stack>
        <div className="ml-6">
          <AuthzChallengeLoggingSection />
        </div>
      </div>
    </>
  );
}
