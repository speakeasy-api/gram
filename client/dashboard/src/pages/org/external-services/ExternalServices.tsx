import { Page } from "@/components/page-layout";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Dialog } from "@/components/ui/Dialog";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import type { ExternalCredentialSummary } from "@gram/client/models/components/externalcredentialsummary.js";
import { useDeleteGcpIamCredentialMutation } from "@gram/client/react-query/deleteGcpIamCredential";
import {
  invalidateAllListExternalCredentials,
  useListExternalCredentials,
} from "@gram/client/react-query/listExternalCredentials";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useVerifyGcpIamCredentialMutation } from "@gram/client/react-query/verifyGcpIamCredential";
import { Button } from "@/components/ui/Button";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { CreateExternalCredentialSheet } from "./CreateExternalCredentialSheet";
import { providerLabel, providerSlug } from "./providers";

export function ExternalServicesRoot(): JSX.Element {
  return <Outlet />;
}

// The probe reports the principal it resolved, but an identity source that
// carries no service-account email is still a successful verify.
function verifiedMessage(principal: string | undefined): string {
  if (!principal) {
    return "Verified.";
  }
  return `Speakeasy can impersonate ${principal}.`;
}

export function ExternalServicesPage(): JSX.Element {
  // Gate first so no protected request (product-feature read, credential list)
  // fires for a visitor lacking the page scope.
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <ExternalServicesGate />
    </RequireScope>
  );
}

// Product-feature (entitlement) gate, mounted only after the RBAC scope gate
// passes. A gated-but-authorized org sees only the framed refusal, with no
// header or toolbar.
function ExternalServicesGate(): JSX.Element {
  const { data: features, isLoading: featuresLoading } = useProductFeatures(
    undefined,
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );

  // The sidebar entry is already hidden without the entitlement; this covers a
  // direct URL. Treat "still loading" and "the read failed" as not-yet-known so
  // an entitled organization never flashes the gate.
  const gated =
    !featuresLoading &&
    features?.customerManagedEncryptionKeysEnabled === false;

  if (gated) {
    return (
      <Page>
        <Page.Header>
          <Page.Header.Breadcrumbs />
        </Page.Header>
        <Page.Body>
          <Text muted className="py-8 text-center">
            Customer-managed keys are not enabled for this organization.
          </Text>
        </Page.Body>
      </Page>
    );
  }

  return <ExternalServicesOverview />;
}

function ExternalServicesOverview(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const [createOpen, setCreateOpen] = useState(false);
  // GCP is the only provider with a detail page today, so scope the list to it
  // rather than linking rows to a route that cannot render them. AWS is deferred
  // until Gram has an AWS identity to assume a customer role from.
  const { data, isLoading, isError, refetch } = useListExternalCredentials({
    provider: "gcp_iam",
  });
  const credentials = data?.credentials ?? [];

  const verify = useVerifyGcpIamCredentialMutation();

  const handleVerify = (credential: ExternalCredentialSummary) => {
    const toastId = toast.loading(`Verifying ${credential.name}…`);
    verify.mutate(
      {
        security: { sessionHeaderGramSession: "" },
        request: { id: credential.id },
      },
      {
        onSuccess: (result) => {
          if (!result.verified) {
            toast.error(result.detail || "Credential could not be verified.", {
              id: toastId,
            });
            return;
          }
          toast.success(verifiedMessage(result.principal), { id: toastId });
        },
        onError: (error) => {
          toast.error(
            error instanceof Error ? error.message : "Verification failed.",
            { id: toastId },
          );
        },
      },
    );
  };

  return (
    <>
      <ResourceListPage
        title="External Services"
        description="How Speakeasy authenticates into your cloud account to reach the keys you manage there. Speakeasy impersonates a service account you nominate, so it never holds long-lived credentials of your own."
        primaryAction={
          <RequireScope scope="org:admin" level="component">
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>New External Credential</Button.Text>
            </Button>
          </RequireScope>
        }
      >
        <CredentialTable
          credentials={credentials}
          isLoading={isLoading}
          isError={isError}
          onRetry={() => void refetch()}
          onVerify={handleVerify}
          detailHref={(credential) =>
            orgRoutes.externalServices.credentialDetail.href(
              providerSlug(credential.provider),
              credential.id,
            )
          }
        />
      </ResourceListPage>

      <CreateExternalCredentialSheet
        open={createOpen}
        onOpenChange={setCreateOpen}
      />
    </>
  );
}

function CredentialTable({
  credentials,
  isLoading,
  isError,
  onRetry,
  onVerify,
  detailHref,
}: {
  credentials: ExternalCredentialSummary[];
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  onVerify: (credential: ExternalCredentialSummary) => void;
  detailHref: (credential: ExternalCredentialSummary) => string;
}): JSX.Element {
  if (isError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Text muted>Failed to load external credentials.</Text>
        <Button size="sm" variant="secondary" onClick={onRetry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }
  if (!isLoading && credentials.length === 0) {
    return (
      <Text muted className="py-8 text-center">
        No external credentials yet
      </Text>
    );
  }

  const headers = [
    { label: "Name" },
    { label: "External Service" },
    { label: "Created" },
    { label: "" },
  ];

  return (
    <DotTable headers={headers}>
      {credentials.map((credential) => (
        <DotRow
          key={credential.id}
          icon={<Icon name="cloud" className="text-muted-foreground h-5 w-5" />}
          href={detailHref(credential)}
          ariaLabel={`View external credential ${credential.name}`}
        >
          <td className="px-3 py-3">
            <Text
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {credential.name}
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted>
              {providerLabel(credential.provider)}
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted as="div">
              <HumanizeDateTime date={credential.createdAt} />
            </Text>
          </td>
          <td className="px-3 py-3 text-right">
            <RowActions credential={credential} onVerify={onVerify} />
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

// RowActions renders the per-row overflow menu. The wrapper stops click
// propagation and sits above the DotRow link so opening the menu (or picking
// an item) never navigates to the detail page.
function RowActions({
  credential,
  onVerify,
}: {
  credential: ExternalCredentialSummary;
  onVerify: (credential: ExternalCredentialSummary) => void;
}): JSX.Element {
  return (
    <div className="relative z-20" onClick={(e) => e.stopPropagation()}>
      {/* The menu portals out of the DOM subtree, so CSS containment cannot
          disable it — gate the trigger by rendering nothing without the scope. */}
      <RequireScope scope="org:admin" level="section">
        <DropdownMenu modal={false}>
          <DropdownMenuTrigger asChild>
            <Button variant="tertiary" size="sm">
              <Button.LeftIcon>
                <MoreHorizontal className="h-4 w-4" />
              </Button.LeftIcon>
            </Button>
          </DropdownMenuTrigger>
          <DropdownMenuContent align="end">
            <DropdownMenuItem onClick={() => onVerify(credential)}>
              Verify
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </div>
  );
}

// DeleteCredentialDialog confirms and performs a credential delete. Shared with
// the detail page's Settings tab (Danger Zone).
export function DeleteCredentialDialog({
  credentialId,
  credentialName,
  onClose,
  onDeleted,
}: {
  credentialId: string;
  credentialName: string;
  onClose: () => void;
  onDeleted: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const deleteMutation = useDeleteGcpIamCredentialMutation({
    onSuccess: async () => {
      await invalidateAllListExternalCredentials(queryClient);
      toast.success("External credential deleted");
      onClose();
      onDeleted();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete credential",
      );
    },
  });

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
    >
      <Dialog.Content>
        <Dialog.Header>
          <Dialog.Title>Delete external credential</Dialog.Title>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          <Text variant="body">
            Are you sure you want to delete{" "}
            <span className="font-bold italic">{credentialName}</span>? This
            action cannot be undone.
          </Text>
          <div className="flex justify-end space-x-2">
            <Button variant="secondary" onClick={onClose}>
              <Button.Text>Cancel</Button.Text>
            </Button>
            <Button
              variant="destructive-primary"
              onClick={() =>
                deleteMutation.mutate({
                  security: { sessionHeaderGramSession: "" },
                  request: { id: credentialId },
                })
              }
              disabled={deleteMutation.isPending}
            >
              <Button.Text>Delete</Button.Text>
            </Button>
          </div>
        </div>
      </Dialog.Content>
    </Dialog>
  );
}
