import { InlineEmptyState } from "@/components/inline-empty-state";
import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { CreateJsonWebKeySetSheet } from "@/pages/org/encryption-keys/jwks/CreateJsonWebKeySetSheet";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListExternalKeysQuery } from "@gram/client/react-query/listExternalKeys";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import type { KeySetChoice } from "../model/serviceAccount";
import { useOrganizationKeySets } from "../queries/useKeySets";
import { RadioRows } from "./RadioRows";

/**
 * Picks the key set a service account client signs its token requests with:
 * any set in the organization, with the one another client at the same
 * identity provider already uses suggested. "Create one" opens the new key set
 * panel straight away and selects what it creates, so an organization's first
 * client does not dead-end.
 */
export function KeySetPicker({
  value,
  onChange,
  suggestedSetId,
  suggestionLoading = false,
  newSetPlaceholder,
  disabled = false,
}: {
  value: KeySetChoice | null;
  /** Null when a chosen set is gone and there is none left to start on. */
  onChange: (choice: KeySetChoice | null) => void;
  /** A set another client at the same identity provider signs with. */
  suggestedSetId?: string | null;
  /** Still looking for a suggestion; the default waits rather than guess. */
  suggestionLoading?: boolean;
  /** The name a new set starts with. */
  newSetPlaceholder: string;
  disabled?: boolean;
}): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const gramClient = useGramContext();
  const setsQuery = useOrganizationKeySets(true);
  const sets = useMemo(() => setsQuery.data?.sets ?? [], [setsQuery.data]);
  const [creating, setCreating] = useState(false);

  // The backing key's name says more than the set's id about what signs.
  const keysQuery = buildListExternalKeysQuery(gramClient, {
    provider: "gcp_kms",
  });
  const { data: keysData } = useQuery({
    ...keysQuery,
    queryKey: [...keysQuery.queryKey, { organizationId: organization.id }],
  });
  const keyNames = new Map(
    (keysData?.keys ?? []).map((key) => [key.id, key.name]),
  );

  // Start on the suggestion, else the first set. A chosen set that is no
  // longer listed, such as one deleted elsewhere, starts over the same way;
  // not mid-refetch, so a set just created is kept until the list has it.
  const loaded = !setsQuery.isPending && !setsQuery.isError;
  const missing =
    value?.kind === "existing" &&
    !setsQuery.isFetching &&
    !sets.some((set) => set.id === value.id);
  useEffect(() => {
    if (!loaded || suggestionLoading) return;
    if (value && !missing) return;
    // Nothing to start over on, so the missing set is not kept as a choice.
    if (sets.length === 0) {
      if (missing) onChange(null);
      return;
    }
    const suggested = sets.find((set) => set.id === suggestedSetId);
    onChange({ kind: "existing", id: (suggested ?? sets[0]!).id });
  }, [
    value,
    missing,
    loaded,
    suggestionLoading,
    sets,
    suggestedSetId,
    onChange,
  ]);

  const openCreate = () => setCreating(true);
  const sheet = (
    <CreateJsonWebKeySetSheet
      open={creating}
      onOpenChange={setCreating}
      isCurrentOrganization={isCurrentOrganization}
      defaultName={newSetPlaceholder}
      onCreated={(created) => onChange({ kind: "existing", id: created.id })}
    />
  );

  if (setsQuery.isError) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label>Key set</Label>
        <div className="flex items-center gap-2">
          <Text small muted>
            Couldn&apos;t load your key sets.
          </Text>
          <Button
            size="sm"
            variant="secondary"
            onClick={() => void setsQuery.refetch()}
          >
            <Button.Text>Retry</Button.Text>
          </Button>
        </div>
      </div>
    );
  }

  if (setsQuery.isPending) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label>Key set</Label>
        <Text small muted>
          Loading key sets…
        </Text>
      </div>
    );
  }

  if (sets.length === 0) {
    return (
      <>
        <InlineEmptyState
          orientation="horizontal"
          icon="key-round"
          heading="No key sets yet"
          description="A key set publishes a signing key from your Google Cloud KMS."
          action={
            <Button size="sm" disabled={disabled} onClick={openCreate}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>Create a key set</Button.Text>
            </Button>
          }
        />
        {sheet}
      </>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <Label>Key set</Label>
      <Text small muted>
        Select a key set or{" "}
        <button
          type="button"
          disabled={disabled}
          onClick={openCreate}
          className="hover:text-foreground underline underline-offset-2 disabled:cursor-not-allowed"
        >
          create one
        </button>
        .
      </Text>
      <RadioRows
        label="Key set"
        value={value?.kind === "existing" ? value.id : null}
        disabled={disabled}
        onValueChange={(id) => onChange({ kind: "existing", id })}
        rows={sets.map((set) => ({
          value: set.id,
          title: set.name,
          description: keyNames.get(set.externalKeyId),
        }))}
      />
      {sheet}
    </div>
  );
}
