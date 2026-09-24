import { Button } from "@/components/ui/Button";
import { Label } from "@/components/ui/Label";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Stack } from "@/components/ui/Stack";
import { Text } from "@/components/ui/Text";
import { useOrganization } from "@/contexts/Auth";
import { useIsCurrentOrganization } from "@/hooks/useIsCurrentOrganization";
import { useGramContext } from "@gram/client/react-query/_context.js";
import { buildListExternalKeysQuery } from "@gram/client/react-query/listExternalKeys";
import { useQuery } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useEffect, useState } from "react";
import { CreateExternalKeySheet } from "../CreateExternalKeySheet";

// ExternalKeySelect picks the KMS key a signing key set publishes from. Shared
// by the create sheet and the publish dialog so both offer the same set and
// explain an empty one the same way.
//
// A key can be registered from here rather than only from the Encryption Keys
// list, because a set cannot exist without one and an organization's first set
// would otherwise dead-end on an empty picker.
//
// Scoped to GCP: the server refuses to back a set with an AWS KMS key, and the
// only algorithms an external key can record (RS256, ES256) are both ones the
// server can publish, so no further filtering is needed.
export function ExternalKeySelect({
  value,
  onChange,
  label = "Encryption key",
  disabled = false,
  unavailableKeyIds,
  unavailableReason = "unavailable",
}: {
  value: string;
  onChange: (value: string) => void;
  label?: string;
  disabled?: boolean;
  // Keys to list but not offer, with the reason shown beside each. Listing
  // them keeps the picker honest about which keys exist; refusing them keeps a
  // save the server would reject from being attempted.
  unavailableKeyIds?: ReadonlySet<string>;
  unavailableReason?: string;
}): JSX.Element {
  const organization = useOrganization();
  const isCurrentOrganization = useIsCurrentOrganization(organization.id);
  const [createOpen, setCreateOpen] = useState(false);
  const client = useGramContext();
  // Keyed by organization so a switch never offers the previous one's keys.
  const keysQuery = buildListExternalKeysQuery(client, { provider: "gcp_kms" });
  const { data, isPending, isError, refetch } = useQuery({
    ...keysQuery,
    queryKey: [...keysQuery.queryKey, { organizationId: organization.id }],
  });
  const keys = data?.keys ?? [];

  // The server refuses to delete a key a set still references, so a selected
  // id normally is in the list. Still, a value the live list does not contain
  // (a caller's stale cache, a key deleted between renders) would otherwise sit
  // invisibly in the empty Select and be resubmitted, so clear it once the
  // live set is known. A failed request is not a live set, so a selection
  // survives a load error.
  //
  // The same applies to a value the caller has since marked unavailable: a key
  // picked while the unavailable set was still loading must not stay selected
  // once the picker says it cannot be used.
  const missing =
    !isPending && !isError && value !== "" && !keys.some((k) => k.id === value);
  const unavailableSelection =
    value !== "" && (unavailableKeyIds?.has(value) ?? false);
  useEffect(() => {
    if (missing || unavailableSelection) onChange("");
  }, [missing, unavailableSelection, onChange]);

  // Select what was just created. The list query is invalidated by the sheet,
  // so the option is there by the time this renders again.
  const createSheet = (
    <CreateExternalKeySheet
      open={createOpen}
      onOpenChange={setCreateOpen}
      isCurrentOrganization={isCurrentOrganization}
      onCreated={(key) => onChange(key.id)}
    />
  );

  if (isError) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label>{label}</Label>
        <Stack direction="horizontal" gap={2} align="center">
          <Text small muted>
            Failed to load encryption keys.
          </Text>
          <Button size="sm" variant="secondary" onClick={() => void refetch()}>
            <Button.Text>Retry</Button.Text>
          </Button>
        </Stack>
      </div>
    );
  }

  if (!isPending && keys.length === 0) {
    return (
      <div className="flex flex-col gap-1.5">
        <Label>{label}</Label>
        <Text small muted>
          A signing key set publishes the public half of a key in your KMS.
          Register one now and it will be selected here.
        </Text>
        <div>
          <Button
            size="sm"
            variant="secondary"
            disabled={disabled}
            onClick={() => setCreateOpen(true)}
          >
            <Button.LeftIcon>
              <Plus />
            </Button.LeftIcon>
            <Button.Text>New KMS Key</Button.Text>
          </Button>
        </div>
        {createSheet}
      </div>
    );
  }

  return (
    <div className="flex flex-col gap-1.5">
      <Label>{label}</Label>
      <Stack direction="horizontal" gap={2} align="center">
        <div className="flex-1">
          <Select
            value={value}
            onValueChange={onChange}
            disabled={isPending || disabled}
          >
            <SelectTrigger>
              <SelectValue
                placeholder={isPending ? "Loading…" : "Select a key"}
              />
            </SelectTrigger>
            <SelectContent>
              {keys.map((key) => {
                const unavailable = unavailableKeyIds?.has(key.id) ?? false;
                return (
                  <SelectItem
                    key={key.id}
                    value={key.id}
                    disabled={unavailable}
                  >
                    {key.name} ({key.algorithm})
                    {unavailable && ` · ${unavailableReason}`}
                  </SelectItem>
                );
              })}
            </SelectContent>
          </Select>
        </div>
        <Button
          size="sm"
          variant="secondary"
          disabled={isPending || disabled}
          onClick={() => setCreateOpen(true)}
        >
          <Button.LeftIcon>
            <Plus />
          </Button.LeftIcon>
          <Button.Text>New</Button.Text>
        </Button>
      </Stack>
      {createSheet}
    </div>
  );
}
