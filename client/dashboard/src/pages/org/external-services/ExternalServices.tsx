import { Page } from "@/components/page-layout";
import { Dialog } from "@/components/ui/dialog";
import { DotRow } from "@/components/ui/dot-row";
import { DotTable } from "@/components/ui/dot-table";
import { Type } from "@/components/ui/type";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import type { ExternalCredentialSummary } from "@gram/client/models/components/externalcredentialsummary.js";
import { useDeleteGcpIamPlatformCredentialMutation } from "@gram/client/react-query/deleteGcpIamPlatformCredential";
import {
  invalidateAllListPlatformExternalCredentials,
  useListPlatformExternalCredentials,
} from "@gram/client/react-query/listPlatformExternalCredentials";
import { useVerifyGcpIamPlatformCredentialMutation } from "@gram/client/react-query/verifyGcpIamPlatformCredential";
import {
  Button,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Icon,
  Stack,
} from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { CreateExternalCredentialSheet } from "./CreateExternalCredentialSheet";
import { providerLabel, verifySourceLabel } from "./providers";

export function ExternalServicesRoot(): JSX.Element {
  return <Outlet />;
}

export function ExternalServicesPage(): JSX.Element {
  const isAdmin = useIsPlatformAdmin();
  return (
    <Page>
      <Page.Header>
        <Page.Header.Breadcrumbs />
      </Page.Header>
      <Page.Body>
        {isAdmin ? (
          <ExternalServicesOverview />
        ) : (
          <Type muted className="py-8 text-center">
            External Services is available to platform admins only.
          </Type>
        )}
      </Page.Body>
    </Page>
  );
}

function ExternalServicesOverview(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const [createOpen, setCreateOpen] = useState(false);
  // GCP is the only provider with a platform-admin create endpoint and detail
  // page today, so scope the list to it to stay consistent with the GCP-only
  // detail route.
  const { data, isLoading, isError, refetch } =
    useListPlatformExternalCredentials({ provider: "gcp_iam" });
  const credentials = data?.credentials ?? [];

  const verify = useVerifyGcpIamPlatformCredentialMutation();

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
          const via = result.identitySource
            ? ` via ${verifySourceLabel(result.identitySource)}`
            : "";
          toast.success(
            result.principal
              ? `Verified as ${result.principal}${via}.`
              : `Verified${via}.`,
            { id: toastId },
          );
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
      <Page.Section>
        <Page.Section.Title>External Services</Page.Section.Title>
        <Page.Section.CTA>
          <Button size="sm" onClick={() => setCreateOpen(true)}>
            <Button.LeftIcon>
              <Plus />
            </Button.LeftIcon>
            <Button.Text>New External Credential</Button.Text>
          </Button>
        </Page.Section.CTA>
        <Page.Section.Description className="max-w-2xl">
          Speakeasy's platform-level credentials for authenticating into
          external services. Shared across every organization and visible to
          platform admins only.
        </Page.Section.Description>
        <Page.Section.Body>
          <CredentialTable
            credentials={credentials}
            isLoading={isLoading}
            isError={isError}
            onRetry={() => void refetch()}
            onVerify={handleVerify}
            detailHref={(id) =>
              orgRoutes.externalServices.credentialDetail.href(id)
            }
          />
        </Page.Section.Body>
      </Page.Section>

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
  detailHref: (id: string) => string;
}): JSX.Element {
  if (isError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Type muted>Failed to load external credentials.</Type>
        <Button size="sm" variant="secondary" onClick={onRetry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }
  if (!isLoading && credentials.length === 0) {
    return (
      <Type muted className="py-8 text-center">
        No external credentials yet
      </Type>
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
          href={detailHref(credential.id)}
          ariaLabel={`View external credential ${credential.name}`}
        >
          <td className="px-3 py-3">
            <Type
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {credential.name}
            </Type>
          </td>
          <td className="px-3 py-3">
            <Type small muted>
              {providerLabel(credential.provider)}
            </Type>
          </td>
          <td className="px-3 py-3">
            <Type small muted as="div">
              <HumanizeDateTime date={credential.createdAt} />
            </Type>
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
    </div>
  );
}

// DeleteCredentialDialog confirms and performs a platform credential delete.
// Shared with the detail page's Settings tab (Danger Zone).
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
  const deleteMutation = useDeleteGcpIamPlatformCredentialMutation({
    onSuccess: async () => {
      await invalidateAllListPlatformExternalCredentials(queryClient);
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
          <Type variant="body">
            Are you sure you want to delete{" "}
            <span className="font-bold italic">{credentialName}</span>? This
            action cannot be undone.
          </Type>
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
