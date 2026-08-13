import { Page } from "@/components/page-layout";
import { ResourceListPage } from "@/components/page-templates";
import { RequireScope } from "@/components/require-scope";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/Dropdown";
import { Icon } from "@/components/ui/Icon";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { HumanizeDateTime } from "@/lib/dates";
import { useOrgRoutes } from "@/routes";
import type { ExternalKeySummary } from "@gram/client/models/components/externalkeysummary.js";
import { useDeleteGcpKmsKeyMutation } from "@gram/client/react-query/deleteGcpKmsKey";
import {
  invalidateAllListExternalKeys,
  useListExternalKeys,
} from "@gram/client/react-query/listExternalKeys";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useVerifyGcpKmsKeyMutation } from "@gram/client/react-query/verifyGcpKmsKey";
import { useQueryClient } from "@tanstack/react-query";
import { MoreHorizontal, Plus } from "lucide-react";
import { useState } from "react";
import { Outlet } from "react-router";
import { toast } from "sonner";
import { CreateExternalKeySheet } from "./CreateExternalKeySheet";
import { providerLabel, providerSlug } from "./providers";

export function EncryptionKeysRoot(): JSX.Element {
  return <Outlet />;
}

export function EncryptionKeysPage(): JSX.Element {
  // Gate first so no protected request (product-feature read, key list) fires
  // for a visitor lacking the page scope.
  return (
    <RequireScope scope={["org:read", "org:admin"]} level="page">
      <EncryptionKeysGate />
    </RequireScope>
  );
}

// Product-feature (entitlement) gate, mounted only after the RBAC scope gate
// passes. A gated-but-authorized org sees only the framed refusal, with no
// header or toolbar.
function EncryptionKeysGate(): JSX.Element {
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

  return <EncryptionKeysOverview />;
}

function EncryptionKeysOverview(): JSX.Element {
  const orgRoutes = useOrgRoutes();
  const [createOpen, setCreateOpen] = useState(false);
  // GCP is the only provider with a detail page today, so scope the list to it
  // rather than linking rows to a route that cannot render them.
  const { data, isLoading, isError, refetch } = useListExternalKeys({
    provider: "gcp_kms",
  });
  const keys = data?.keys ?? [];

  return (
    <>
      <ResourceListPage
        title="Encryption Keys"
        description="The keys in your own cloud KMS that Gram signs with. Gram never holds the key material: it reaches each key through an external credential you configure and asks your KMS to sign."
        primaryAction={
          <RequireScope scope="org:admin" level="component">
            <Button size="sm" onClick={() => setCreateOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>New KMS Key</Button.Text>
            </Button>
          </RequireScope>
        }
      >
        <KeyTable
          keys={keys}
          isLoading={isLoading}
          isError={isError}
          onRetry={() => void refetch()}
          detailHref={(key) =>
            orgRoutes.encryptionKeys.keyDetail.href(
              providerSlug(key.provider),
              key.id,
            )
          }
        />
      </ResourceListPage>

      <CreateExternalKeySheet open={createOpen} onOpenChange={setCreateOpen} />
    </>
  );
}

// useVerifyGcpKmsKey returns a handler that probes one key and reports the
// outcome as a toast. The detail page renders the same probe as a panel instead,
// because there it is the point of the page rather than a row action.
//
// The hook is called per row rather than once for the table on purpose. A single
// mutation observer serves one call at a time — starting a second probe detaches
// the first — and a verify is a real KMS round trip, so an operator working down
// the list would leave the earlier rows' toasts spinning forever and never see
// their outcome. mutateAsync keeps each toast id in its own call closure.
function useVerifyGcpKmsKey(externalKey: ExternalKeySummary): {
  verify: () => void;
  isPending: boolean;
} {
  const mutation = useVerifyGcpKmsKeyMutation();

  const verify = () => {
    const toastId = toast.loading(`Verifying ${externalKey.name}…`);
    mutation
      .mutateAsync({
        security: { sessionHeaderGramSession: "" },
        request: { id: externalKey.id },
      })
      .then((result) => {
        if (!result.verified) {
          toast.error(result.detail || "Key could not be verified.", {
            id: toastId,
          });
          return;
        }
        toast.success(`${externalKey.name} signed and verified.`, {
          id: toastId,
        });
      })
      .catch((error: unknown) => {
        toast.error(
          error instanceof Error ? error.message : "Verification failed.",
          { id: toastId },
        );
      });
  };

  return { verify, isPending: mutation.isPending };
}

function KeyTable({
  keys,
  isLoading,
  isError,
  onRetry,
  detailHref,
}: {
  keys: ExternalKeySummary[];
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  detailHref: (key: ExternalKeySummary) => string;
}): JSX.Element {
  if (isError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Text muted>Failed to load encryption keys.</Text>
        <Button size="sm" variant="secondary" onClick={onRetry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }
  if (!isLoading && keys.length === 0) {
    return (
      <Text muted className="py-8 text-center">
        No encryption keys yet
      </Text>
    );
  }

  const headers = [
    { label: "Name" },
    { label: "Provider" },
    { label: "Algorithm" },
    { label: "Created" },
    { label: "" },
  ];

  return (
    <DotTable headers={headers}>
      {keys.map((key) => (
        <DotRow
          key={key.id}
          icon={
            <Icon name="key-square" className="text-muted-foreground h-5 w-5" />
          }
          href={detailHref(key)}
          ariaLabel={`View encryption key ${key.name}`}
        >
          <td className="px-3 py-3">
            <Text
              variant="subheading"
              as="div"
              className="group-hover:text-primary truncate text-sm transition-colors group-hover:underline"
            >
              {key.name}
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted>
              {providerLabel(key.provider)}
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted>
              {key.algorithm}
            </Text>
          </td>
          <td className="px-3 py-3">
            <Text small muted as="div">
              <HumanizeDateTime date={key.createdAt} />
            </Text>
          </td>
          <td className="px-3 py-3 text-right">
            <RowActions externalKey={key} />
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
  externalKey,
}: {
  externalKey: ExternalKeySummary;
}): JSX.Element {
  const { verify, isPending } = useVerifyGcpKmsKey(externalKey);

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
            <DropdownMenuItem disabled={isPending} onClick={verify}>
              {isPending ? "Verifying…" : "Verify"}
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </RequireScope>
    </div>
  );
}

// DeleteKeyDialog confirms and performs a key delete. Shared with the detail
// page's Settings tab (Danger Zone).
export function DeleteKeyDialog({
  keyId,
  keyName,
  onClose,
  onDeleted,
}: {
  keyId: string;
  keyName: string;
  onClose: () => void;
  onDeleted: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const deleteMutation = useDeleteGcpKmsKeyMutation({
    onSuccess: async () => {
      await invalidateAllListExternalKeys(queryClient);
      toast.success("Encryption key deleted");
      onClose();
      onDeleted();
    },
    onError: (error) => {
      toast.error(
        error instanceof Error ? error.message : "Failed to delete key",
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
          <Dialog.Title>Delete encryption key</Dialog.Title>
        </Dialog.Header>
        <div className="space-y-4 py-4">
          <Text variant="body">
            Are you sure you want to delete{" "}
            <span className="font-bold italic">{keyName}</span>? This action
            cannot be undone. The key itself is untouched in your KMS; Gram just
            stops being able to sign with it.
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
                  request: { id: keyId },
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
