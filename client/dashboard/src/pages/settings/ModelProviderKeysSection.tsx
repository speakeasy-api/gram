import { ReleaseStageBadge } from "@/components/release-stage-badge";
import { Heading } from "@/components/ui/heading";
import { SkeletonTable } from "@/components/ui/skeleton";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Type } from "@/components/ui/type";
import { useProjectSlugForRequests } from "@/contexts/Sdk";
import { HumanizeDateTime } from "@/lib/dates";
import type { ModelProviderKey } from "@gram/client/models/components/modelproviderkey.js";
import { useDeleteModelProviderKeyMutation } from "@gram/client/react-query/deleteModelProviderKey.js";
import {
  invalidateAllModelProviderKeys,
  useModelProviderKeys,
} from "@gram/client/react-query/modelProviderKeys.js";
import { useProductFeatures } from "@gram/client/react-query/productFeatures.js";
import { useSetModelProviderKeyEnabledMutation } from "@gram/client/react-query/setModelProviderKeyEnabled.js";
import { useUpsertModelProviderKeyMutation } from "@gram/client/react-query/upsertModelProviderKey.js";
import { useQueryClient } from "@tanstack/react-query";
import {
  Badge,
  Button,
  Column,
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
  Input,
  Stack,
  Table,
} from "@speakeasy-api/moonshine";
import { Check, MoreHorizontal } from "lucide-react";
import { type ComponentProps, useMemo, useState } from "react";
import { toast } from "sonner";
import {
  keySourceForSlot,
  MODEL_KEY_PROVIDERS,
  MODEL_KEY_SLOTS,
  type KeySource,
  type ModelKeyProvider,
  type ModelKeySlot,
} from "./model-key-slots";

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
  const [draftValues, setDraftValues] = useState<Record<string, string>>({});
  const [draftProviders, setDraftProviders] = useState<
    Record<string, ModelKeyProvider>
  >({});

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

  const { mutate: upsertKey, isPending: isSaving } =
    useUpsertModelProviderKeyMutation({
      onSuccess: () => {
        toast.success("Provider key saved");
        void invalidateAllModelProviderKeys(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to save key: ${err.message}`);
      },
    });

  const { mutate: setKeyEnabled, isPending: isSettingEnabled } =
    useSetModelProviderKeyEnabledMutation({
      onSuccess: (key) => {
        toast.success(`Provider key ${key.enabled ? "enabled" : "disabled"}`);
        void invalidateAllModelProviderKeys(queryClient);
      },
      onError: (err) => {
        toast.error(`Failed to update key: ${err.message}`);
      },
    });

  const handleRemove = (slot: ModelKeySlot, key: ModelProviderKey) => {
    if (!window.confirm(`Remove the ${slot.name} provider key?`)) return;
    deleteKey(
      { request: { id: key.id } },
      {
        onSuccess: () => {
          setDraftValues((values) => ({ ...values, [slot.slot]: "" }));
        },
      },
    );
  };

  const handleSave = (slot: ModelKeySlot, key?: ModelProviderKey) => {
    const apiKey = draftValues[slot.slot]?.trim();
    if (!apiKey || isSaving) return;
    const provider =
      draftProviders[slot.slot] ?? providerForSlot(slot.slot, keysBySlot);

    upsertKey(
      {
        request: {
          upsertKeyRequestBody: {
            slot: slot.slot,
            provider,
            apiKey,
            enabled: key?.enabled ?? true,
          },
        },
      },
      {
        onSuccess: () => {
          setDraftValues((values) => ({ ...values, [slot.slot]: "" }));
        },
      },
    );
  };

  const handleValueChange = (slot: ModelKeySlot, value: string) => {
    setDraftValues((values) => ({ ...values, [slot.slot]: value }));
  };

  const handleProviderChange = (
    slot: ModelKeySlot,
    provider: ModelKeyProvider,
  ) => {
    setDraftProviders((providers) => ({
      ...providers,
      [slot.slot]: provider,
    }));
  };

  const handleSetEnabled = (key: ModelProviderKey) => {
    setKeyEnabled({
      request: {
        setKeyEnabledRequestBody: {
          id: key.id,
          enabled: !key.enabled,
        },
      },
    });
  };

  const isMutating = isDeleting || isSaving || isSettingEnabled;

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
      width: "160px",
      render: (slot) => (
        <KeySourceBadge source={keySourceForSlot(slot.slot, keysBySlot)} />
      ),
    },
    {
      key: "provider",
      header: "Provider",
      width: "150px",
      render: (slot) => {
        const provider =
          draftProviders[slot.slot] ?? providerForSlot(slot.slot, keysBySlot);
        return (
          <Select
            value={provider}
            onValueChange={(value) =>
              handleProviderChange(slot, value as ModelKeyProvider)
            }
            disabled={isMutating || !customModelKeysEnabled}
          >
            <SelectTrigger
              className="h-9 w-full"
              aria-label={`${slot.name} key provider`}
            >
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {MODEL_KEY_PROVIDERS.map((option) => (
                <SelectItem key={option.value} value={option.value}>
                  {option.label}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
        );
      },
    },
    {
      key: "value",
      header: "Value",
      width: "260px",
      render: (slot) => {
        const key = keysBySlot.get(slot.slot);
        const draftValue = draftValues[slot.slot] ?? "";
        const provider =
          draftProviders[slot.slot] ?? providerForSlot(slot.slot, keysBySlot);
        const isDirty = draftValue.length > 0;
        return (
          <div className="relative">
            <Input
              type="password"
              value={draftValue}
              placeholder={
                key ? "••••••••••••" : keyPlaceholderForProvider(provider)
              }
              className="h-9 py-0 pr-10"
              onChange={(event) => handleValueChange(slot, event.target.value)}
              onKeyDown={(event) => {
                if (event.key !== "Enter") return;
                event.preventDefault();
                handleSave(slot, key);
              }}
              disabled={isMutating || !customModelKeysEnabled}
              aria-label={`${slot.name} key value`}
            />
            {isDirty ? (
              <Button
                type="button"
                variant="tertiary"
                size="sm"
                className="border-input bg-muted hover:bg-muted/80 absolute top-0 right-0 h-9 rounded-l-none border shadow-none"
                onClick={() => handleSave(slot, key)}
                disabled={
                  isMutating ||
                  !customModelKeysEnabled ||
                  draftValue.trim() === ""
                }
                aria-label={`Save ${slot.name} key`}
                title="Save key"
              >
                <Button.Icon>
                  <Check className="stroke-success-default size-4" />
                </Button.Icon>
              </Button>
            ) : null}
          </div>
        );
      },
    },
    {
      key: "updated",
      header: "Updated",
      width: "160px",
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
      width: "64px",
      render: (slot) => {
        const key = keysBySlot.get(slot.slot);
        if (!key) return null;

        return (
          <div className="flex justify-end">
            <DropdownMenu modal={false}>
              <DropdownMenuTrigger asChild>
                <Button
                  variant="tertiary"
                  size="sm"
                  disabled={isMutating}
                  aria-label={`${slot.name} key actions`}
                >
                  <Button.Icon>
                    <MoreHorizontal className="size-4" />
                  </Button.Icon>
                </Button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end">
                <DropdownMenuItem
                  onSelect={() => handleSetEnabled(key)}
                  disabled={!key.enabled && !customModelKeysEnabled}
                >
                  {key.enabled ? "Disable" : "Enable"}
                </DropdownMenuItem>
                <DropdownMenuItem
                  className="text-destructive focus:text-destructive cursor-pointer"
                  onSelect={() => handleRemove(slot, key)}
                >
                  Delete
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        );
      },
    },
  ];

  if (
    !customModelKeysEnabled &&
    !isLoading &&
    !isError &&
    (data?.keys.length ?? 0) === 0
  ) {
    return null;
  }

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

  return (
    <Stack gap={4}>
      <div>
        <Stack direction="horizontal" gap={2} align="center" className="mb-2">
          <Heading variant="h4">Model Provider Keys</Heading>
          <ReleaseStageBadge stage="preview" />
        </Stack>
        <Type muted small>
          Bring your own OpenRouter or Anthropic API key for model completions.
          Set a project default for all surfaces, or override individual
          surfaces. Keys are write-only and never displayed after saving.
        </Type>
      </div>

      {keyList}
    </Stack>
  );
}

function providerForKey(key?: ModelProviderKey): ModelKeyProvider {
  if (key?.provider === "anthropic") return "anthropic";
  return "openrouter";
}

function providerForSlot(
  slot: string,
  keysBySlot: Map<string, ModelProviderKey>,
): ModelKeyProvider {
  const ownKey = keysBySlot.get(slot);
  if (ownKey) return providerForKey(ownKey);
  if (slot !== "default") return providerForKey(keysBySlot.get("default"));
  return "openrouter";
}

function keyPlaceholderForProvider(provider: ModelKeyProvider): string {
  switch (provider) {
    case "anthropic":
      return "sk-ant-…";
    case "openrouter":
      return "sk-or-…";
  }
}

const KEY_SOURCE_BADGE: Record<
  KeySource,
  { variant: ComponentProps<typeof Badge>["variant"]; label: string }
> = {
  custom: { variant: "success", label: "Custom key" },
  inherited: { variant: "information", label: "Project default" },
  platform: { variant: "neutral", label: "Platform issued" },
};

function KeySourceBadge({ source }: { source: KeySource }): JSX.Element {
  const badge = KEY_SOURCE_BADGE[source];
  return (
    <Badge variant={badge.variant} background className="shrink-0">
      <Badge.Text>{badge.label}</Badge.Text>
    </Badge>
  );
}
