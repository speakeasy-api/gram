import { Page } from "@/components/page-layout";
import { RequireScope } from "@/components/require-scope";
import { ErrorAlert } from "@/components/ui/alert";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/field";
import { Input } from "@/components/ui/input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Skeleton } from "@/components/ui/skeleton";
import { Type } from "@/components/ui/type";
import { useRBAC } from "@/hooks/useRBAC";
import type { DeviceAgentConfiguration } from "@gram/client/models/components/deviceagentconfiguration.js";
import {
  invalidateAllDeviceAgentConfiguration,
  useDeviceAgentConfiguration,
} from "@gram/client/react-query/deviceAgentConfiguration.js";
import { useUpdateDeviceAgentConfigurationMutation } from "@gram/client/react-query/updateDeviceAgentConfiguration.js";
import { Button, Stack } from "@speakeasy-api/moonshine";
import { useQueryClient } from "@tanstack/react-query";
import { type ReactNode, useState } from "react";
import { toast } from "sonner";

type EnforcementLayer = "off" | "user" | "managed";

const PLATFORMS = [
  {
    key: "claude_code",
    label: "Claude Code",
    description: "Configure Claude Code plugins and MCP settings.",
    defaultLayer: "user",
  },
  {
    key: "codex",
    label: "Codex",
    description: "Configure Codex plugins and MCP settings.",
    defaultLayer: "off",
  },
  {
    key: "cursor",
    label: "Cursor",
    description: "Configure Cursor plugins and MCP settings.",
    defaultLayer: "off",
  },
] as const satisfies ReadonlyArray<{
  key: string;
  label: string;
  description: string;
  defaultLayer: EnforcementLayer;
}>;

const MIN_SYNC_INTERVAL_SECONDS = 60;
const MAX_SYNC_INTERVAL_SECONDS = 86_400;

function recordValue(value: unknown): Record<string, unknown> {
  if (typeof value === "object" && value !== null && !Array.isArray(value)) {
    return value as Record<string, unknown>;
  }
  return {};
}

function enforcementLayer(
  config: DeviceAgentConfiguration,
  platform: (typeof PLATFORMS)[number],
): EnforcementLayer {
  const value = recordValue(config.config.platforms)[platform.key];
  if (value === "user" || value === "managed") return value;
  if (value === false) return "off";
  return platform.defaultLayer;
}

function stringSetting(
  config: DeviceAgentConfiguration,
  key: string,
  fallback: string,
): string {
  const value = config.config[key];
  return typeof value === "string" ? value : fallback;
}

function syncIntervalSetting(config: DeviceAgentConfiguration): string {
  const value = config.config.sync_interval_seconds;
  return typeof value === "number" ? String(value) : "60";
}

function blockedVersionsSetting(config: DeviceAgentConfiguration): string {
  const value = config.config.blocked_versions;
  if (!Array.isArray(value)) return "";
  return value
    .filter((item): item is string => typeof item === "string")
    .join(", ");
}

function syncIntervalError(value: string): string | undefined {
  const parsed = Number(value);
  if (!Number.isInteger(parsed)) return "Enter a whole number.";
  if (
    parsed < MIN_SYNC_INTERVAL_SECONDS ||
    parsed > MAX_SYNC_INTERVAL_SECONDS
  ) {
    return `Enter a value from ${MIN_SYNC_INTERVAL_SECONDS} to ${MAX_SYNC_INTERVAL_SECONDS}.`;
  }
  return undefined;
}

export function DeviceAgentConfigurationTab(): JSX.Element {
  const query = useDeviceAgentConfiguration(undefined, undefined, {
    throwOnError: false,
  });

  if (query.isLoading || !query.data) {
    return (
      <ConfigurationSection>
        {query.error ? (
          <Stack gap={4}>
            <ErrorAlert
              title="Unable to load device agent configuration"
              error={query.error}
              className="max-w-2xl"
            />
            <Button variant="secondary" onClick={() => void query.refetch()}>
              Try again
            </Button>
          </Stack>
        ) : (
          <Skeleton className="h-[640px] w-full max-w-2xl" />
        )}
      </ConfigurationSection>
    );
  }

  return (
    <ConfigurationSection>
      <DeviceAgentConfigurationForm
        key={query.data.etag}
        configuration={query.data}
      />
    </ConfigurationSection>
  );
}

function ConfigurationSection({ children }: { children: ReactNode }) {
  return (
    <Page.Section>
      <Page.Section.Title stage="preview">
        Fleet configuration
      </Page.Section.Title>
      <Page.Section.Description>
        Set non-secret device agent behavior once for every enrolled machine in
        this organization.
      </Page.Section.Description>
      <Page.Section.Body>{children}</Page.Section.Body>
    </Page.Section>
  );
}

function DeviceAgentConfigurationForm({
  configuration,
}: {
  configuration: DeviceAgentConfiguration;
}): JSX.Element {
  const queryClient = useQueryClient();
  const { hasScope } = useRBAC();
  const canEdit = hasScope("org:admin");
  const [platformLayers, setPlatformLayers] = useState<
    Record<string, EnforcementLayer>
  >(() =>
    Object.fromEntries(
      PLATFORMS.map((platform) => [
        platform.key,
        enforcementLayer(configuration, platform),
      ]),
    ),
  );
  const [updateChannel, setUpdateChannel] = useState(() =>
    stringSetting(configuration, "update_channel", "stable"),
  );
  const [autoUpdate, setAutoUpdate] = useState(() =>
    stringSetting(configuration, "auto_update", "notify"),
  );
  const [syncInterval, setSyncInterval] = useState(() =>
    syncIntervalSetting(configuration),
  );
  const [pinnedTarget, setPinnedTarget] = useState(() =>
    stringSetting(configuration, "pinned_target", ""),
  );
  const [blockedVersions, setBlockedVersions] = useState(() =>
    blockedVersionsSetting(configuration),
  );
  const [saveError, setSaveError] = useState<string>();

  const mutation = useUpdateDeviceAgentConfigurationMutation({
    onSuccess: async () => {
      setSaveError(undefined);
      await invalidateAllDeviceAgentConfiguration(queryClient);
      toast.success("Device agent configuration saved");
    },
    onError: (error) => {
      setSaveError(error.message);
      toast.error("Failed to save device agent configuration");
    },
  });

  const intervalError = syncIntervalError(syncInterval);
  const currentPlatformLayers = Object.fromEntries(
    PLATFORMS.map((platform) => [
      platform.key,
      enforcementLayer(configuration, platform),
    ]),
  );
  const isDirty =
    JSON.stringify(platformLayers) !== JSON.stringify(currentPlatformLayers) ||
    updateChannel !==
      stringSetting(configuration, "update_channel", "stable") ||
    autoUpdate !== stringSetting(configuration, "auto_update", "notify") ||
    syncInterval !== syncIntervalSetting(configuration) ||
    pinnedTarget !== stringSetting(configuration, "pinned_target", "") ||
    blockedVersions !== blockedVersionsSetting(configuration);
  const disabled = mutation.isPending || !canEdit;

  const handleSave = () => {
    if (intervalError) return;

    const config = { ...configuration.config };
    const existingPlatforms = recordValue(config.platforms);
    config.platforms = {
      ...existingPlatforms,
      ...Object.fromEntries(
        Object.entries(platformLayers).map(([platform, layer]) => [
          platform,
          layer === "off" ? false : layer,
        ]),
      ),
    };
    config.update_channel = updateChannel.trim();
    config.auto_update = autoUpdate.trim();
    config.sync_interval_seconds = Number(syncInterval);

    if (pinnedTarget.trim()) {
      config.pinned_target = pinnedTarget.trim();
    } else {
      delete config.pinned_target;
    }
    const versions = blockedVersions
      .split(",")
      .map((version) => version.trim())
      .filter(Boolean);
    if (versions.length > 0) {
      config.blocked_versions = versions;
    } else {
      delete config.blocked_versions;
    }

    setSaveError(undefined);
    mutation.mutate({
      request: {
        updateConfigurationRequestBody: { config },
      },
    });
  };

  return (
    <div className="border-border bg-card max-w-2xl rounded-lg border p-6">
      <Stack gap={6}>
        <div>
          <Type variant="body" className="font-medium">
            Managed tools
          </Type>
          <Type muted small>
            Choose where the agent enforces each tool&apos;s configuration.
          </Type>
        </div>

        <div className="divide-border divide-y">
          {PLATFORMS.map((platform) => (
            <div
              key={platform.key}
              className="flex items-center justify-between gap-6 py-4 first:pt-0 last:pb-0"
            >
              <div>
                <Type variant="body" className="font-medium">
                  {platform.label}
                </Type>
                <Type muted small>
                  {platform.description}
                </Type>
              </div>
              <Select
                value={platformLayers[platform.key]}
                onValueChange={(value) =>
                  setPlatformLayers((current) => ({
                    ...current,
                    [platform.key]: value as EnforcementLayer,
                  }))
                }
                disabled={disabled}
              >
                <SelectTrigger
                  className="w-40"
                  aria-label={`${platform.label} enforcement layer`}
                >
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="off">Off</SelectItem>
                  <SelectItem
                    value="user"
                    description="Write to the user's home directory"
                  >
                    User
                  </SelectItem>
                  <SelectItem
                    value="managed"
                    description="Use the elevated managed writer"
                  >
                    Managed
                  </SelectItem>
                </SelectContent>
              </Select>
            </div>
          ))}
        </div>

        <div className="border-border border-t" />

        <div className="grid gap-6 sm:grid-cols-2">
          <Field>
            <FieldLabel htmlFor="device-agent-update-channel">
              Update channel
            </FieldLabel>
            <Input
              id="device-agent-update-channel"
              value={updateChannel}
              onChange={setUpdateChannel}
              disabled={disabled}
              placeholder="stable"
            />
            <FieldDescription>
              Release channel agents should follow.
            </FieldDescription>
          </Field>

          <Field>
            <FieldLabel htmlFor="device-agent-auto-update">
              Auto-update policy
            </FieldLabel>
            <Input
              id="device-agent-auto-update"
              value={autoUpdate}
              onChange={setAutoUpdate}
              disabled={disabled}
              placeholder="notify"
            />
            <FieldDescription>
              Update mode understood by deployed agent versions.
            </FieldDescription>
          </Field>

          <Field data-invalid={intervalError ? true : undefined}>
            <FieldLabel htmlFor="device-agent-sync-interval">
              Sync interval
            </FieldLabel>
            <Input
              id="device-agent-sync-interval"
              type="number"
              min={MIN_SYNC_INTERVAL_SECONDS}
              max={MAX_SYNC_INTERVAL_SECONDS}
              step={1}
              value={syncInterval}
              onChange={setSyncInterval}
              disabled={disabled}
              aria-invalid={intervalError ? true : undefined}
            />
            <FieldDescription>
              Seconds between reconciliations.
            </FieldDescription>
            <FieldError>{intervalError}</FieldError>
          </Field>

          <Field>
            <FieldLabel htmlFor="device-agent-pinned-target">
              Pinned target
            </FieldLabel>
            <Input
              id="device-agent-pinned-target"
              value={pinnedTarget}
              onChange={setPinnedTarget}
              disabled={disabled}
              placeholder="Optional version"
            />
            <FieldDescription>
              Pin the fleet to a specific agent release.
            </FieldDescription>
          </Field>
        </div>

        <Field>
          <FieldLabel htmlFor="device-agent-blocked-versions">
            Blocked versions
          </FieldLabel>
          <Input
            id="device-agent-blocked-versions"
            value={blockedVersions}
            onChange={setBlockedVersions}
            disabled={disabled}
            placeholder="1.2.3, 1.2.4"
          />
          <FieldDescription>
            Comma-separated agent releases that must not be installed.
          </FieldDescription>
        </Field>

        <div className="bg-muted/40 rounded-md border p-4">
          <Type muted small>
            After the first successful fetch, these settings override the same
            non-secret fields from local and MDM configuration. Device identity
            and credentials always remain local. If Gram is temporarily
            unreachable, agents use their last-known remote configuration; an
            agent without a cached remote configuration falls back to local
            settings.
          </Type>
        </div>

        {!configuration.isConfigured && (
          <Type muted small>
            No remote configuration is active yet. Saving enables this layer for
            enrolled agents.
          </Type>
        )}

        {saveError && (
          <ErrorAlert
            title="Unable to save device agent configuration"
            error={saveError}
            onDismiss={() => setSaveError(undefined)}
          />
        )}

        <RequireScope
          scope="org:admin"
          level="component"
          reason="Organization admin access is required to change fleet configuration."
        >
          <Button
            variant="primary"
            onClick={handleSave}
            disabled={!isDirty || Boolean(intervalError) || mutation.isPending}
          >
            {mutation.isPending ? "Saving..." : "Save configuration"}
          </Button>
        </RequireScope>
      </Stack>
    </div>
  );
}
