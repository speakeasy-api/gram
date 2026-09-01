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
import { useGramContext } from "@gram/client/react-query/_context.js";
import { useAttachOrganizationRemoteSessionClientKeySetMutation } from "@gram/client/react-query/attachOrganizationRemoteSessionClientKeySet.js";
import { useDetachOrganizationRemoteSessionClientKeySetMutation } from "@gram/client/react-query/detachOrganizationRemoteSessionClientKeySet.js";
import { buildListJsonWebKeySetsQuery } from "@gram/client/react-query/listJsonWebKeySets";
import { invalidateAllOrganizationRemoteSessionClient } from "@gram/client/react-query/organizationRemoteSessionClient.js";
import { useOrganizationRemoteSessionClients } from "@gram/client/react-query/organizationRemoteSessionClients.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { toast } from "sonner";

// The Select cannot carry an empty string as a value, so "no set attached" gets
// a sentinel. Chosen to be impossible as a uuid.
const NONE = "__none__";

// KeySetField picks the JSON Web Key Set this client signs private_key_jwt
// assertions with. It sits beside the token endpoint authentication method
// because that is where an administrator is already deciding how Gram
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
}: {
  client: RemoteSessionClient;
  issuerId: string;
}): JSX.Element | null {
  const organization = useOrganization();
  const gramClient = useGramContext();
  const queryClient = useQueryClient();

  const { data: features, isLoading: featuresLoading } = useProductFeatures(
    { organizationId: organization.id },
    undefined,
    { staleTime: 30_000, throwOnError: false },
  );
  const entitled =
    !featuresLoading && features?.customerManagedEncryptionKeysEnabled === true;

  // Keyed by organization so a switch never offers the previous one's sets.
  const setsQuery = buildListJsonWebKeySetsQuery(gramClient);
  const { data: setsData, isPending: setsPending } = useQuery({
    ...setsQuery,
    queryKey: [...setsQuery.queryKey, { organizationId: organization.id }],
    enabled: entitled,
  });
  const sets = setsData?.sets ?? [];

  // AIM-64 establishes that Gram may hold more than one client at the same
  // downstream issuer — an interactive one and a separate chaining one. Same
  // trust relationship, same counterparty, so when a sibling at this issuer
  // already signs with a set, suggest that one rather than making the
  // administrator remember which of their sets this issuer expects.
  //
  // First page only (the list defaults to 50). This is a hint, not a
  // constraint, so an issuer with more clients than that simply does not get
  // one; paginating to find a suggestion would not earn its complexity.
  const { data: siblingsData } = useOrganizationRemoteSessionClients(
    { issuerId },
    undefined,
    { throwOnError: false, enabled: entitled },
  );
  const suggestedSetId = (siblingsData?.result.items ?? [])
    .map((item) => item.client)
    .find(
      (sibling) => sibling.id !== client.id && sibling.jsonWebKeySetId != null,
    )?.jsonWebKeySetId;

  const onSettled = async (message: string) => {
    await invalidateAllOrganizationRemoteSessionClient(queryClient, {
      refetchType: "all",
    });
    toast.success(message);
  };

  // Refetch on failure too. A 404 here usually means the set was deleted from
  // another tab, and without this the picker keeps offering the dead set.
  const onError = async (error: unknown) => {
    toast.error(
      error instanceof Error ? error.message : "Failed to update signing key",
    );
    await invalidateAllOrganizationRemoteSessionClient(queryClient, {
      refetchType: "all",
    });
    await queryClient.invalidateQueries({ queryKey: setsQuery.queryKey });
  };

  const attach = useAttachOrganizationRemoteSessionClientKeySetMutation({
    onSuccess: () => onSettled("Signing key set attached"),
    onError,
  });
  const detach = useDetachOrganizationRemoteSessionClientKeySetMutation({
    onSuccess: () => onSettled("Signing key set detached"),
    onError,
  });

  if (!entitled) return null;

  const pending = attach.isPending || detach.isPending;
  const selected = client.jsonWebKeySetId ?? NONE;

  const handleChange = (next: string) => {
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
        disabled={setsPending || pending}
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
        </SelectContent>
      </Select>
      {sets.length === 0 && !setsPending ? (
        <Text small muted>
          No signing key sets yet. Create one under Encryption Keys to let this
          client authenticate with a signed assertion instead of a shared
          secret.
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
