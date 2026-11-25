import { Button } from "@/components/ui/Button";
import { Input } from "@/components/ui/Input";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import type { RemoteSessionClient } from "@gram/client/models/components/remotesessionclient.js";
import { GramError } from "@gram/client/models/errors/gramerror.js";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAttachOrganizationRemoteSessionClientKeySetMutation } from "@gram/client/react-query/attachOrganizationRemoteSessionClientKeySet.js";
import { useDetachOrganizationRemoteSessionClientKeySetMutation } from "@gram/client/react-query/detachOrganizationRemoteSessionClientKeySet.js";
import { invalidateAllListJsonWebKeySets } from "@gram/client/react-query/listJsonWebKeySets";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { buildOrganizationRemoteSessionClientsQuery } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
  useCustomerManagedKeys,
  useOrganizationKeySets,
  useResolveKeySet,
} from "@/lib/remote-identity";
import { ExternalKeySelect } from "@/pages/org/encryption-keys/jwks/ExternalKeySelect";
import { useEffect, useState } from "react";
import { toast } from "sonner";

// The Select cannot carry an empty string as a value, so "no set attached" gets
// a sentinel. Chosen to be impossible as a uuid.
const NONE = "__none__";
// Opens the inline form that creates a set from a Google Cloud KMS key.
const NEW_SET = "__new__";

// KeySetField picks the JSON Web Key Set this client signs private_key_jwt
// assertions with. It sits beside the token endpoint authentication method
// because that is where an administrator is already deciding how Speakeasy
// authenticates at this counterparty.
//
// Unlike the other fields on this tab it saves immediately rather than on "Save
// changes": the link has its own attach/detach endpoints, which is what lets the
// server gate it on the customer_managed_encryption_keys entitlement without
// gating ordinary client updates, and lets it refuse a detach that would strand
// a private_key_jwt client.
//
// Renders nothing at all without the entitlement. The framed refusal
// CustomerManagedKeysGate shows is right for a whole page, but wrong for one
// field inside a form that stays perfectly usable: an organization without the
// entitlement has no key sets and no way to create one, so the control has
// nothing to offer.
export function KeySetField({
  client,
  issuerId,
  onPendingChange,
  disabled = false,
}: {
  client: RemoteSessionClient;
  issuerId: string;
  // Lets the enclosing form hold its own Save until this link has settled, so
  // an update that depends on the attach cannot overtake it.
  onPendingChange?: (pending: boolean) => void;
  disabled?: boolean;
}): JSX.Element | null {
  const organization = useOrganization();
  const gramClient = useGramContext();
  const queryClient = useQueryClient();

  const { entitled } = useCustomerManagedKeys();
  // Keyed by organization so a switch never offers the previous one's sets.
  const { data: setsData, isPending: setsPending } =
    useOrganizationKeySets(entitled);
  const sets = setsData?.sets ?? [];

  // AIM-64 establishes that Speakeasy may hold more than one client at the same
  // downstream issuer — an interactive one and a separate chaining one. Same
  // trust relationship, same counterparty, so when a sibling at this issuer
  // already signs with a set, suggest that one rather than making the
  // administrator remember which of their sets this issuer expects.
  //
  // First page only (the list defaults to 50). This is a hint, not a
  // constraint, so an issuer with more clients than that simply does not get
  // one; paginating to find a suggestion would not earn its complexity.
  const siblingsQuery = buildOrganizationRemoteSessionClientsQuery(gramClient, {
    issuerId,
  });
  const { data: siblingsData } = useQuery({
    ...siblingsQuery,
    queryKey: [...siblingsQuery.queryKey, { organizationId: organization.id }],
    enabled: entitled,
  });
  const suggestedSetId = (siblingsData?.result.items ?? [])
    .map((item) => item.client)
    .find(
      (sibling) => sibling.id !== client.id && sibling.jsonWebKeySetId != null,
    )?.jsonWebKeySetId;

  // An organization switch mid-flight must not invalidate the new
  // organization's caches or toast about the old one's client, so each callback
  // re-checks the organization it started in.
  const settledIn = (organizationId: string) =>
    organizationId === organization.id;

  const onSettled = (organizationId: string) => async (message: string) => {
    if (!settledIn(organizationId)) return;
    await invalidateAllOrganizationRemoteSessionClient(queryClient, {
      refetchType: "all",
    });
    toast.success(message);
  };

  // Refetch on failure too. A 404 here usually means the set was deleted from
  // another tab, and without this the picker keeps offering the dead set.
  const onError = (organizationId: string) => async (error: unknown) => {
    if (!settledIn(organizationId)) return;
    toast.error(
      error instanceof Error ? error.message : "Failed to update signing key",
    );
    await invalidateAllOrganizationRemoteSessionClient(queryClient, {
      refetchType: "all",
    });
    await invalidateAllListJsonWebKeySets(queryClient);
  };

  const startedIn = organization.id;
  const attach = useAttachOrganizationRemoteSessionClientKeySetMutation({
    onSuccess: () => onSettled(startedIn)("Signing key set attached"),
    onError: onError(startedIn),
  });
  const detach = useDetachOrganizationRemoteSessionClientKeySetMutation({
    onSuccess: () => onSettled(startedIn)("Signing key set detached"),
    onError: onError(startedIn),
  });

  const resolveKeySet = useResolveKeySet();
  const [newSetOpen, setNewSetOpen] = useState(false);
  const [newSetName, setNewSetName] = useState("");
  const [newSetKeyId, setNewSetKeyId] = useState("");
  const [creatingSet, setCreatingSet] = useState(false);
  // A set created by an attempt whose attach failed, reused by a retry with
  // the same name and key rather than created again.
  const [madeSet, setMadeSet] = useState<{
    id: string;
    name: string;
    externalKeyId: string;
  } | null>(null);

  const createAndAttach = async () => {
    setCreatingSet(true);
    try {
      let jsonWebKeySetId = madeSet?.id;
      if (
        !jsonWebKeySetId ||
        madeSet?.name !== newSetName ||
        madeSet.externalKeyId !== newSetKeyId
      ) {
        try {
          jsonWebKeySetId = await resolveKeySet({
            kind: "new",
            name: newSetName,
            externalKeyId: newSetKeyId,
          });
        } catch (error) {
          toast.error(
            error instanceof Error ? error.message : "Failed to create key set",
          );
          return;
        }
        setMadeSet({
          id: jsonWebKeySetId,
          name: newSetName,
          externalKeyId: newSetKeyId,
        });
      }
      try {
        await attach.mutateAsync({
          request: { attachKeySetForm: { id: client.id, jsonWebKeySetId } },
        });
      } catch (error) {
        // The attach mutation's onError has already reported it. A 404 means
        // the set (or the client) is gone, likely deleted elsewhere, so drop the
        // cached set: reusing its id would fail the same way on every retry.
        if (error instanceof GramError && error.statusCode === 404) {
          setMadeSet(null);
        }
        return;
      }
      setMadeSet(null);
      setNewSetOpen(false);
      setNewSetName("");
      setNewSetKeyId("");
    } finally {
      setCreatingSet(false);
    }
  };

  const pending = attach.isPending || detach.isPending || creatingSet;
  const selected = client.jsonWebKeySetId ?? NONE;

  useEffect(() => {
    onPendingChange?.(pending);
  }, [pending, onPendingChange]);

  if (!entitled) return null;

  const handleChange = (next: string) => {
    if (next === NEW_SET) {
      setNewSetOpen(true);
      return;
    }
    setNewSetOpen(false);
    if (next === selected) return;

    if (next === NONE) {
      detach.mutate({ request: { id: client.id } });
      return;
    }

    attach.mutate({
      request: {
        attachKeySetForm: { id: client.id, jsonWebKeySetId: next },
      },
    });
  };

  const suggestion =
    selected === NONE && suggestedSetId != null
      ? sets.find((set) => set.id === suggestedSetId)
      : undefined;

  return (
    <div className="flex flex-col gap-1.5">
      <Label>Signing key set</Label>
      <Select
        value={selected}
        onValueChange={handleChange}
        disabled={disabled || setsPending || pending}
      >
        <SelectTrigger>
          <SelectValue placeholder={setsPending ? "Loading…" : "None"} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={NONE}>None</SelectItem>
          {sets.map((set) => (
            <SelectItem key={set.id} value={set.id}>
              {set.name}
              {/* Radix renders the selected item's children into the trigger, so
                  the hint is suppressed once this set is the current value —
                  otherwise the field reads "MySet · used by another client here". */}
              {set.id === suggestedSetId &&
                set.id !== selected &&
                " · used by another client here"}
            </SelectItem>
          ))}
          <SelectItem value={NEW_SET}>Set up a new key set…</SelectItem>
        </SelectContent>
      </Select>
      {newSetOpen ? (
        <div className="bg-muted/20 flex flex-col gap-3 border p-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="client-new-key-set-name">Name</Label>
            <Input
              id="client-new-key-set-name"
              value={newSetName}
              onChange={setNewSetName}
              placeholder="Production signing keys"
              disabled={pending}
            />
          </div>
          <ExternalKeySelect
            label="Google Cloud KMS key"
            value={newSetKeyId}
            onChange={setNewSetKeyId}
            disabled={pending}
          />
          <div className="flex gap-2">
            <Button
              size="sm"
              disabled={
                pending || newSetName.trim() === "" || newSetKeyId === ""
              }
              onClick={() => void createAndAttach()}
            >
              <Button.Text>
                {creatingSet ? "Creating…" : "Create and attach"}
              </Button.Text>
            </Button>
            <Button
              size="sm"
              variant="secondary"
              disabled={pending}
              onClick={() => setNewSetOpen(false)}
            >
              <Button.Text>Cancel</Button.Text>
            </Button>
          </div>
        </div>
      ) : sets.length === 0 && !setsPending ? (
        <Text small muted>
          No signing key sets yet. Set up a new one from a Google Cloud KMS key
          to let this client authenticate with a signed assertion instead of a
          shared secret.
        </Text>
      ) : (
        <Text small muted>
          The key set whose private half signs this client's assertions at the
          issuer's token endpoint. Leave unset to authenticate with a shared
          secret.
          {suggestion &&
            ` Another client at this issuer already uses ${suggestion.name}.`}
        </Text>
      )}
    </div>
  );
}
