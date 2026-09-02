import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { Text } from "@/components/ui/Text";
import { ConfirmDialog } from "@/pages/remote-identity-providers/ConfirmDialog";
import type { JSONWebKey } from "@gram/client/models/components/jsonwebkey.js";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useActivateJsonWebKeyMutation } from "@gram/client/react-query/activateJsonWebKey";
import { useJsonWebKeySetDeletePreflight } from "@gram/client/react-query/jsonWebKeySetDeletePreflight.js";
import { useDeleteJsonWebKeySetMutation } from "@gram/client/react-query/deleteJsonWebKeySet";
import {
  buildGetJsonWebKeySetQuery,
  invalidateAllGetJsonWebKeySet,
} from "@gram/client/react-query/getJsonWebKeySet";
import { useListJsonWebKeys } from "@gram/client/react-query/listJsonWebKeys";
import { usePublishJsonWebKeyMutation } from "@gram/client/react-query/publishJsonWebKey";
import { useRetireJsonWebKeyMutation } from "@gram/client/react-query/retireJsonWebKey";
import { useRevokeJsonWebKeyMutation } from "@gram/client/react-query/revokeJsonWebKey";
import { useUpdateJsonWebKeySetMutation } from "@gram/client/react-query/updateJsonWebKeySet";
import { useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { toast } from "sonner";
import { ExternalKeySelect } from "./ExternalKeySelect";
import { invalidateSet } from "./invalidate";
import { keyActionCopy, type KeyLifecycleAction } from "./keyLifecycle";

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

// KeyActionDialog confirms and performs one lifecycle transition on a published
// key. The three mutations are all instantiated because hooks cannot be chosen
// per render; only the one for `action` is ever fired.
export function KeyActionDialog({
  action,
  jsonWebKey,
  onClose,
}: {
  action: KeyLifecycleAction;
  jsonWebKey: JSONWebKey;
  onClose: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  const copy = keyActionCopy(action);

  const options = {
    onSuccess: async () => {
      await invalidateSet(queryClient);
      toast.success(copy.successMessage);
      onClose();
    },
  };
  const activate = useActivateJsonWebKeyMutation(options);
  const retire = useRetireJsonWebKeyMutation(options);
  const revoke = useRevokeJsonWebKeyMutation(options);

  const mutation = { activate, retire, revoke }[action];

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={copy.title}
      description={
        <>
          {copy.description}{" "}
          <span className="font-mono text-xs break-all">{jsonWebKey.kid}</span>
        </>
      }
      confirmLabel={copy.confirmLabel}
      confirmVariant={copy.destructive ? "destructive-primary" : "primary"}
      isPending={mutation.isPending}
      error={
        mutation.error
          ? errorMessage(mutation.error, "The key could not be updated.")
          : null
      }
      onConfirm={() =>
        mutation.mutate({
          security: { sessionHeaderGramSession: "" },
          request: { id: jsonWebKey.id },
        })
      }
    />
  );
}

// DeleteSetDialog confirms and performs a set delete. Deleting withdraws every
// key in the set at once, so the copy treats it as decommissioning a trust
// anchor rather than removing a record.
//
// The preflight is loaded before the administrator confirms, because deleteSet
// refuses outright while any live remote_session_client still signs with the
// set. Discovering that only after confirming a dialog this alarming is a bad
// trade: the refusal is knowable up front, so the dialog names the blocking
// clients and disables the button rather than letting the request fail.
export function DeleteSetDialog({
  set,
  onClose,
  onDeleted,
}: {
  set: JSONWebKeySet;
  onClose: () => void;
  onDeleted: () => void;
}): JSX.Element {
  const queryClient = useQueryClient();
  // isFetching, not isPending: a reopened dialog has cached preflight data, so
  // isPending is false while the refetch is still in flight and Delete would be
  // confirmable against whatever the last open saw.
  const {
    data: preflight,
    isFetching: preflightPending,
    isError: preflightFailed,
  } = useJsonWebKeySetDeletePreflight({ id: set.id }, undefined, {
    throwOnError: false,
  });
  const deleteMutation = useDeleteJsonWebKeySetMutation({
    onSuccess: async () => {
      await invalidateSet(queryClient);
      toast.success("Signing key set deleted");
      onClose();
      onDeleted();
    },
  });

  const blockingCount = preflight?.clientCount ?? 0;
  // A failed preflight leaves this false, so the delete is still attempted and
  // the server's own refusal stops it. Better that the authoritative check
  // answers than that an advisory read's error blocks a legitimate delete. The
  // summary below says so rather than claiming nothing references the set.
  const blocked = blockingCount > 0;

  return (
    <ConfirmDialog
      open
      onOpenChange={(open) => {
        if (!open) onClose();
      }}
      title={`Delete "${set.name}"?`}
      description="Every key in this set is withdrawn immediately and tokens signed with any of them stop verifying. Anything that trusts this set, such as an identity provider or MCP server verifying against its JWKS, will start rejecting those tokens. This cannot be undone; the keys themselves stay in your KMS."
      confirmLabel="Delete key set"
      isPending={deleteMutation.isPending}
      confirmDisabled={blocked}
      impact={{
        summary: blocked
          ? blockingCount === 1
            ? "1 remote session client still signs with this set. Detach it there before deleting."
            : `${blockingCount} remote session clients still sign with this set. Detach it from each before deleting.`
          : preflightFailed
            ? "Could not check which remote session clients reference this set. Deleting is still refused by the server if any do."
            : "No remote session clients reference this set.",
        mcpServerNames: preflight?.clientIds,
        namesLabel: "Clients signing with this set:",
        isLoading: preflightPending,
      }}
      error={
        deleteMutation.error
          ? errorMessage(
              deleteMutation.error,
              "The key set could not be deleted.",
            )
          : null
      }
      onConfirm={() =>
        deleteMutation.mutate({
          security: { sessionHeaderGramSession: "" },
          request: { id: set.id },
        })
      }
    />
  );
}

type PublishPhase =
  // Nothing has been written yet.
  | "idle"
  // The set now points at the chosen key but no key was published from it.
  | "repointed";

function publishButtonLabel(pending: boolean, phase: PublishPhase): string {
  if (pending) return "Publishing…";
  return phase === "repointed" ? "Retry publish" : "Publish key";
}

// PublishKeyDialog publishes a new key into a set. Publishing mints from the
// set's current backing key, and the same KMS key cannot be published twice, so
// in practice publishing means rotating: pick the KMS key to publish from, and
// the set is re-pointed at it first when it differs from the current one.
//
// The two writes are separate requests. If the publish fails after the
// re-point, the set is left pointing at the new key with nothing published from
// it, so the dialog stays open, says so, and a retry only publishes.
export function PublishKeyDialog({
  set,
  onClose,
}: {
  set: JSONWebKeySet;
  onClose: () => void;
}): JSX.Element {
  const client = useGramContext();
  const queryClient = useQueryClient();
  // No default: the set's current backing key has almost always been published
  // already (creation publishes it), so preselecting it would offer a publish
  // the server refuses.
  const [externalKeyId, setExternalKeyId] = useState("");
  const [phase, setPhase] = useState<PublishPhase>("idle");
  const [error, setError] = useState<string | null>(null);

  // Every KMS key with a kid in the set, revoked included, is refused by the
  // server as a duplicate, so the picker lists them but does not offer them.
  const { data: keysData, isFetching: keysFetching } = useListJsonWebKeys(
    { setId: set.id, includeRevoked: true },
    undefined,
    { throwOnError: false },
  );
  const publishedKeyIds = useMemo(
    () => new Set((keysData?.keys ?? []).map((key) => key.externalKeyId)),
    [keysData],
  );

  const update = useUpdateJsonWebKeySetMutation();
  const publish = usePublishJsonWebKeyMutation();
  const pending = update.isPending || publish.isPending;

  const handlePublish = async () => {
    setError(null);
    try {
      if (phase === "idle") {
        // Read the set again rather than trusting the props: the update
        // replaces name and key together, so a stale name here would undo a
        // rename made elsewhere since this page loaded.
        const fresh = await queryClient.fetchQuery(
          buildGetJsonWebKeySetQuery(client, { id: set.id }),
        );
        if (fresh.externalKeyId !== externalKeyId) {
          await update.mutateAsync({
            security: { sessionHeaderGramSession: "" },
            request: {
              updateJSONWebKeySetRequestBody: {
                id: set.id,
                name: fresh.name,
                externalKeyId,
              },
            },
          });
          setPhase("repointed");
          await invalidateAllGetJsonWebKeySet(queryClient);
        }
      }
      await publish.mutateAsync({
        security: { sessionHeaderGramSession: "" },
        request: { setId: set.id },
      });
      await invalidateSet(queryClient);
      toast.success("Key published");
      onClose();
    } catch (caught: unknown) {
      setError(errorMessage(caught, "The key could not be published."));
    }
  };

  return (
    <Dialog
      open
      onOpenChange={(open) => {
        if (!open && !pending) onClose();
      }}
    >
      <Dialog.Content closeable={!pending}>
        <Dialog.Header>
          <Dialog.Title>Publish new key</Dialog.Title>
          <Dialog.Description>
            The new key is published as pending so verifiers can cache it before
            it signs anything; activate it from the Keys tab once they have. If
            the set has no active key, the new key is activated immediately.
          </Dialog.Description>
        </Dialog.Header>
        <div className="flex flex-col gap-4">
          <ExternalKeySelect
            label="Publish from"
            value={externalKeyId}
            onChange={setExternalKeyId}
            disabled={pending || phase === "repointed"}
            unavailableKeyIds={publishedKeyIds}
            unavailableReason="already published in this set"
          />
          <Text small muted>
            A KMS key can only be published into a set once. To rotate, register
            the new key version under Encryption Keys and choose it here; the
            set is re-pointed at it and keys already published keep signing with
            the key they came from.
          </Text>
          {phase === "repointed" && (
            <Alert variant="warning" dismissible={false}>
              The set now publishes from the chosen key, but no key has been
              published from it yet. Retry to publish, or close and publish
              later from the Keys tab.
            </Alert>
          )}
          {error && (
            <Alert variant="error" dismissible={false}>
              {error}
            </Alert>
          )}
        </div>
        <Dialog.Footer>
          <Button variant="tertiary" onClick={onClose} disabled={pending}>
            <Button.Text>Cancel</Button.Text>
          </Button>
          <Button
            variant="primary"
            onClick={() => void handlePublish()}
            // Publishing while the published-key list is still being read
            // (first load or a refetch) could send a key the server refuses as
            // a duplicate; a failed list still lets the server be the judge.
            disabled={pending || keysFetching || externalKeyId === ""}
          >
            <Button.Text>{publishButtonLabel(pending, phase)}</Button.Text>
          </Button>
        </Dialog.Footer>
      </Dialog.Content>
    </Dialog>
  );
}
