import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Button } from "@/components/ui/button";
import { Heading } from "@/components/ui/heading";
import { SkeletonTable } from "@/components/ui/skeleton";
import { Switch } from "@/components/ui/switch";
import { Type } from "@/components/ui/type";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { HumanizeDateTime } from "@/lib/dates";
import type { ModelProviderKey } from "@gram/client/models/components/modelproviderkey.js";
import { useDeleteModelProviderKeyMutation } from "@gram/client/react-query/deleteModelProviderKey.js";
import {
  invalidateAllModelProviderKeys,
  useModelProviderKeys,
} from "@gram/client/react-query/modelProviderKeys.js";
import { useSetModelProviderKeyEnabledMutation } from "@gram/client/react-query/setModelProviderKeyEnabled.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useQueryClient } from "@tanstack/react-query";
import { Badge, Column, Stack, Table } from "@speakeasy-api/moonshine";
import { Trash2 } from "lucide-react";
import { type ComponentProps, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  keySourceForSlot,
  MODEL_KEY_SLOTS,
  type KeySource,
  type ModelKeySlot,
} from "./model-key-slots";
import { ModelProviderKeyDialog } from "./ModelProviderKeyDialog";

export function ModelProviderKeysSection(): JSX.Element | null {
  const { data: features } = useProductFeatures();

  if (!features) {
    return null;
  }

  return (
    <ModelProviderKeysTable
      customModelKeysEnabled={features.customModelKeysEnabled}
    />
  );
}

function ModelProviderKeysTable({
  customModelKeysEnabled,
}: {
  customModelKeysEnabled: boolean;
}): JSX.Element | null {
  const queryClient = useQueryClient();
  const gramProject = useProjectSlugForRequests();
  const { data, isLoading, isError, refetch } = useModelProviderKeys(
    { gramProject },
    undefined,
    { throwOnError: false },
  );
  const [dialogSlot, setDialogSlot] = useState<ModelKeySlot | null>(null);

  const keysBySlot = useMemo(
    () => new Map((data?.keys ?? []).map((key) => [key.slot, key] as const)),
    [data],
  );

  const { mutate: deleteKey, isPending: isDeleting } =
    useDeleteModelProviderKeyMutation({
      onSuccess: () => {
        toast.success("Provider key removed");
        void invalidateAllModelProviderKeys(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to remove key: ${err.message}`);
      },
    });

  const { mutate: setKeyEnabled, isPending: isSettingEnabled } =
    useSetModelProviderKeyEnabledMutation({
      onSuccess: (key) => {
        toast.success(`Provider key ${key.enabled ? "enabled" : "disabled"}`);
        return invalidateAllModelProviderKeys(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to update key: ${err.message}`);
      },
    });

  const handleRemove = (slot: ModelKeySlot, key: ModelProviderKey) => {
    if (!window.confirm(`Remove the ${slot.name} provider key?`)) return;
    deleteKey({ request: { id: key.id } });
  };

  const handleSetEnabled = (key: ModelProviderKey, enabled: boolean) => {
    setKeyEnabled({
      request: {
        setKeyEnabledRequestBody: {
          id: key.id,
          enabled,
        },
      },
    });
  };

  const isMutating = isDeleting || isSettingEnabled;

  if (
    !customModelKeysEnabled &&
    !isLoading &&
    !isError &&
    (data?.keys.length ?? 0) === 0
  ) {
    return null;
  }

  const columns: Column<ModelKeySlot>[] = [
    {
      key: "surface",
      header: "Surface",
      render: (slot) => (
        <Stack gap={1}>
          <Type variant="body" className="font-medium">
            {slot.name}
          </Type>
          <Type muted small>
            {slot.description}
          </Type>
        </Stack>
      ),
    },
    {
      key: "key",
      header: "Key",
      width: "260px",
      render: (slot) => {
        const key = keysBySlot.get(slot.slot);
        return (
          <Stack direction="horizontal" gap={2} align="center">
            <KeySourceBadge source={keySourceForSlot(slot.slot, keysBySlot)} />
            {key && !key.enabled ? (
              <Badge variant="warning" background className="shrink-0">
                <Badge.Text>Disabled</Badge.Text>
              </Badge>
            ) : null}
          </Stack>
        );
      },
    },
    {
      key: "updated",
      header: "Updated",
      width: "170px",
      render: (slot) => {
        const key = keysBySlot.get(slot.slot);
        if (!key) {
          return (
            <Type muted small>
              —
            </Type>
          );
        }
        return (
          <Type muted small className="whitespace-nowrap">
            <HumanizeDateTime date={key.updatedAt} />
          </Type>
        );
      },
    },
    {
      key: "actions",
      header: "",
      width: "220px",
      render: (slot) => {
        const key = keysBySlot.get(slot.slot);
        return (
          <Stack direction="horizontal" gap={2} align="center" justify="end">
            <Button
              variant="secondary"
              size="sm"
              className="w-28"
              onClick={() => setDialogSlot(slot)}
              disabled={isMutating || !customModelKeysEnabled}
            >
              {key ? "Replace key" : "Set key"}
            </Button>
            <div className="flex w-9 shrink-0 justify-center">
              {key ? (
                <Switch
                  checked={key.enabled}
                  onCheckedChange={(enabled) => handleSetEnabled(key, enabled)}
                  disabled={
                    isMutating || (!key.enabled && !customModelKeysEnabled)
                  }
                  aria-label={`${slot.name} key enabled`}
                />
              ) : null}
            </div>
            <div className="flex w-9 shrink-0 justify-center">
              {key ? (
                <Button
                  variant="destructiveGhost"
                  size="sm"
                  onClick={() => handleRemove(slot, key)}
                  disabled={isMutating}
                  aria-label={`Remove ${slot.name} key`}
                >
                  <Trash2 className="h-3.5 w-3.5" />
                </Button>
              ) : null}
            </div>
          </Stack>
        );
      },
    },
  ];

  let keyList: JSX.Element;
  if (isLoading) {
    keyList = <SkeletonTable />;
  } else if (isError) {
    keyList = (
      <Stack direction="horizontal" gap={2} align="center">
        <Type muted small>
          Failed to load provider keys.
        </Type>
        <Button variant="secondary" size="sm" onClick={() => void refetch()}>
          Retry
        </Button>
      </Stack>
    );
  } else {
    keyList = (
      <Table
        columns={columns}
        data={MODEL_KEY_SLOTS}
        rowKey={(row) => row.slot}
      />
    );
  }

  const dialogKey = dialogSlot ? keysBySlot.get(dialogSlot.slot) : undefined;

  return (
    <Stack gap={4}>
      <div>
        <Stack direction="horizontal" gap={2} align="center" className="mb-2">
          <Heading variant="h4">Model Provider Keys</Heading>
          <ReleaseStageBadge stage="preview" />
        </Stack>
        <Type muted small>
          Bring your own OpenRouter API key for model completions. Set a project
          default for all surfaces, or override individual surfaces. Keys are
          write-only and never displayed after saving.
        </Type>
      </div>

      {keyList}

      {dialogSlot ? (
        <ModelProviderKeyDialog
          slot={dialogSlot}
          existingEnabled={dialogKey?.enabled}
          onClose={() => setDialogSlot(null)}
        />
      ) : null}
    </Stack>
  );
}

const KEY_SOURCE_BADGE: Record<
  KeySource,
  { variant: ComponentProps<typeof Badge>["variant"]; label: string }
> = {
  custom: { variant: "success", label: "Custom key" },
  inherited: { variant: "information", label: "Project default" },
  platform: { variant: "neutral", label: "Platform key" },
};

function KeySourceBadge({ source }: { source: KeySource }): JSX.Element {
  const badge = KEY_SOURCE_BADGE[source];
  return (
    <Badge variant={badge.variant} background className="shrink-0">
      <Badge.Text>{badge.label}</Badge.Text>
    </Badge>
  );
}
