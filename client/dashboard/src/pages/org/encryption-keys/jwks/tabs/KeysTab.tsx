import { RequireScope } from "@/components/require-scope";
import { Badge } from "@/components/ui/Badge";
import { Button } from "@/components/ui/Button";
import { CopyButton } from "@/components/ui/CopyButton";
import { DotRow } from "@/components/ui/DotRow";
import { DotTable } from "@/components/ui/DotTable";
import { Icon } from "@/components/ui/Icon";
import { Label } from "@/components/ui/Label";
import { MoreActions, type Action } from "@/components/ui/MoreActions";
import { Stack } from "@/components/ui/Stack";
import { Switch } from "@/components/ui/Switch";
import { Text } from "@/components/ui/Text";
import { SimpleTooltip } from "@/components/ui/Tooltip";
import { HumanizeDateTime } from "@/lib/dates";
import type { JSONWebKey } from "@gram/client/models/components/jsonwebkey.js";
import type { JSONWebKeySet } from "@gram/client/models/components/jsonwebkeyset.js";
import { useListJsonWebKeys } from "@gram/client/react-query/listJsonWebKeys";
import { keepPreviousData } from "@tanstack/react-query";
import { Plus } from "lucide-react";
import { useState } from "react";
import { KeyActionDialog, PublishKeyDialog } from "../dialogs";
import {
  availableKeyActions,
  keyActionCopy,
  keyAlgorithm,
  keyStateBadgeVariant,
  keyStateDescription,
  keyStateLabel,
  type KeyLifecycleAction,
} from "../keyLifecycle";

type PendingAction = { action: KeyLifecycleAction; key: JSONWebKey };

export function KeysTab({ set }: { set: JSONWebKeySet }): JSX.Element {
  const [includeRevoked, setIncludeRevoked] = useState(false);
  const [publishOpen, setPublishOpen] = useState(false);
  const [pendingAction, setPendingAction] = useState<PendingAction | null>(
    null,
  );

  const { data, isLoading, isError, refetch } = useListJsonWebKeys(
    { setId: set.id, includeRevoked },
    undefined,
    // Flipping the revoked toggle changes the query key; keeping the previous
    // rows in place avoids the table blinking to its loading state each time.
    { placeholderData: keepPreviousData },
  );
  const keys = data?.keys ?? [];

  return (
    <div className="flex flex-col gap-6">
      <div className="flex flex-wrap items-center justify-between gap-4">
        <Text small muted className="max-w-3xl">
          The public keys published in this set. Verifiers accept tokens signed
          by any published key; only the active key signs new ones.
        </Text>
        <Stack direction="horizontal" gap={4} align="center">
          <Stack direction="horizontal" gap={2} align="center">
            <Switch
              checked={includeRevoked}
              onCheckedChange={setIncludeRevoked}
              aria-labelledby="jwks-include-revoked"
            />
            <Label id="jwks-include-revoked">Include revoked</Label>
          </Stack>
          <RequireScope scope="org:admin" level="component">
            <Button size="sm" onClick={() => setPublishOpen(true)}>
              <Button.LeftIcon>
                <Plus />
              </Button.LeftIcon>
              <Button.Text>Publish new key</Button.Text>
            </Button>
          </RequireScope>
        </Stack>
      </div>

      <KeyTable
        keys={keys}
        includeRevoked={includeRevoked}
        isLoading={isLoading}
        isError={isError}
        onRetry={() => void refetch()}
        onAction={(action, key) => setPendingAction({ action, key })}
      />

      {publishOpen && (
        <PublishKeyDialog set={set} onClose={() => setPublishOpen(false)} />
      )}
      {pendingAction && (
        <KeyActionDialog
          action={pendingAction.action}
          jsonWebKey={pendingAction.key}
          onClose={() => setPendingAction(null)}
        />
      )}
    </div>
  );
}

function KeyTable({
  keys,
  includeRevoked,
  isLoading,
  isError,
  onRetry,
  onAction,
}: {
  keys: JSONWebKey[];
  includeRevoked: boolean;
  isLoading: boolean;
  isError: boolean;
  onRetry: () => void;
  onAction: (action: KeyLifecycleAction, key: JSONWebKey) => void;
}): JSX.Element {
  if (isError) {
    return (
      <Stack gap={3} className="py-8" align="center" justify="center">
        <Text muted>Failed to load keys.</Text>
        <Button size="sm" variant="secondary" onClick={onRetry}>
          <Button.Text>Retry</Button.Text>
        </Button>
      </Stack>
    );
  }

  if (isLoading) {
    return (
      <Text muted className="py-8 text-center">
        Loading…
      </Text>
    );
  }

  if (keys.length === 0) {
    return (
      <Text muted className="py-8 text-center">
        No published keys. Publish one to make this set usable again.
      </Text>
    );
  }

  const headers = [
    { label: "Key ID" },
    { label: "Algorithm" },
    { label: "State" },
    { label: "Published" },
    { label: "Activated" },
    { label: "Retired" },
    ...(includeRevoked ? [{ label: "Revoked" }] : []),
    { label: "" },
  ];

  return (
    <DotTable headers={headers}>
      {keys.map((key) => (
        <DotRow
          key={key.id}
          icon={
            <Icon name="key-round" className="text-muted-foreground h-5 w-5" />
          }
        >
          <td className="px-3 py-3">
            <Stack direction="horizontal" gap={1} align="center">
              <SimpleTooltip tooltip={key.kid}>
                <Text
                  small
                  as="span"
                  className="inline-block max-w-[16ch] truncate font-mono"
                >
                  {key.kid}
                </Text>
              </SimpleTooltip>
              <CopyButton size="xs" text={key.kid} tooltip="Copy key ID" />
            </Stack>
          </td>
          <td className="px-3 py-3">
            <Text small muted>
              {keyAlgorithm(key)}
            </Text>
          </td>
          <td className="px-3 py-3">
            <SimpleTooltip tooltip={keyStateDescription(key.keyState)}>
              <Badge variant={keyStateBadgeVariant(key.keyState)} size="sm">
                {keyStateLabel(key.keyState)}
              </Badge>
            </SimpleTooltip>
          </td>
          <td className="px-3 py-3">
            <Text small muted as="div">
              <HumanizeDateTime date={key.createdAt} />
            </Text>
          </td>
          <td className="px-3 py-3">
            <OptionalDate date={key.activatedAt} />
          </td>
          <td className="px-3 py-3">
            <OptionalDate date={key.retiredAt} />
          </td>
          {includeRevoked && (
            <td className="px-3 py-3">
              <OptionalDate date={key.revokedAt} />
            </td>
          )}
          <td className="px-3 py-3 text-right">
            <KeyRowActions jsonWebKey={key} onAction={onAction} />
          </td>
        </DotRow>
      ))}
    </DotTable>
  );
}

function OptionalDate({ date }: { date: Date | undefined }): JSX.Element {
  return (
    <Text small muted as="div">
      {date ? <HumanizeDateTime date={date} /> : "—"}
    </Text>
  );
}

// KeyRowActions offers only the transitions the server accepts from the key's
// current state; a revoked key has none and renders no menu at all.
function KeyRowActions({
  jsonWebKey,
  onAction,
}: {
  jsonWebKey: JSONWebKey;
  onAction: (action: KeyLifecycleAction, key: JSONWebKey) => void;
}): JSX.Element | null {
  const actions: Action[] = availableKeyActions(jsonWebKey.keyState).map(
    (action) => ({
      label: keyActionCopy(action).confirmLabel,
      destructive: keyActionCopy(action).destructive,
      onClick: () => onAction(action, jsonWebKey),
    }),
  );

  if (actions.length === 0) return null;

  return (
    <RequireScope scope="org:admin" level="section">
      <MoreActions actions={actions} />
    </RequireScope>
  );
}
