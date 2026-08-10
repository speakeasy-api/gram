import { RequireScope } from "@/components/require-scope";
import { Heading } from "@/components/ui/Heading";
import { InternalAdminBadge } from "@/components/internal-admin-badge";
import { ErrorAlert } from "@/components/ui/Alert";
import {
  Field,
  FieldDescription,
  FieldError,
  FieldLabel,
} from "@/components/ui/Field";
import { Input } from "@/components/ui/Input";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/Select";
import { Skeleton } from "@/components/ui/Skeleton";
import { Text } from "@/components/ui/Text";
import { useIsPlatformAdmin } from "@/contexts/Auth";
import { useRBAC } from "@/hooks/useRBAC";
import type { DeviceAgentConfiguration } from "@gram/client/models/components/deviceagentconfiguration.js";
import {
  invalidateAllDeviceAgentConfiguration,
  useDeviceAgentConfiguration,
} from "@gram/client/react-query/deviceAgentConfiguration.js";
import { useUpdateDeviceAgentConfigurationMutation } from "@gram/client/react-query/updateDeviceAgentConfiguration.js";
import { Button } from "@/components/ui/Button";
import { Stack } from "@/components/ui/Stack";
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
  {
    key: "opencode",
    label: "OpenCode",
    description: "Configure OpenCode plugins and MCP settings.",
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

// Sentinel for "auto_update is unset" — saving it deletes the key so devices
// fall back to their local or MDM setting. Must not collide with a real agent
// update mode.
const AUTO_UPDATE_INHERIT = "inherit";

// Update modes understood by deployed device agent versions.
const AUTO_UPDATE_POLICIES = [
  {
    value: "disabled",
    label: "Disabled",
    description: "Agents never self-update",
  },
  {
    value: "notify",
    label: "Notify",
    description: "Surface available updates without installing",
  },
  {
    value: "automatic",
    label: "Automatic",
    description: "Install updates as they release",
  },
] as const;

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

function autoUpdateSetting(config: DeviceAgentConfiguration): string {
  const value = config.config.auto_update;
  return typeof value === "string" && value.trim()
    ? value
    : AUTO_UPDATE_INHERIT;
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
          <Skeleton className="h-[640px] w-full" />
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

// The Device Agent page already renders the area eyebrow and display-serif
// page title above the tab strip, so this in-tab header stays a plain
// section heading rather than repeating the page-header idiom.
function ConfigurationSection({ children }: { children: ReactNode }) {
  return (
    <Stack gap={6} className="mt-3 mb-6">
      <div>
        <Heading variant="h4" className="mb-2">
          Fleet configuration
        </Heading>
        <Text muted small>
          Set non-secret device agent behavior once for every enrolled machine
          in this organization.
        </Text>
      </div>
      {children}
    </Stack>
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
  const isPlatformAdmin = useIsPlatformAdmin();
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
    autoUpdateSetting(configuration),
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
  // A stored auto_update mode this UI predates — keep it selectable so the
  // dropdown can display it instead of rendering blank.
  const isUnknownAutoUpdate =
    autoUpdate !== AUTO_UPDATE_INHERIT &&
    !AUTO_UPDATE_POLICIES.some((policy) => policy.value === autoUpdate);
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
    autoUpdate !== autoUpdateSetting(configuration) ||
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
    if (autoUpdate === AUTO_UPDATE_INHERIT) {
      delete config.auto_update;
    } else {
      config.auto_update = autoUpdate;
    }
    config.sync_interval_seconds = Number(syncInterval);

    if (pinnedTarget.trim()) {
      config.pinned_target = pinnedTarget.trim();
    } else {
      delete config.pinned_target;
    }

    // Update channel and blocked versions are Speakeasy-internal release
    // controls. Non-admins never see the fields, so leave whatever is stored
    // untouched rather than writing the (unchanged) local state back.
    if (isPlatformAdmin) {
      config.update_channel = updateChannel.trim();

      const versions = blockedVersions
        .split(",")
        .map((version) => version.trim())
        .filter(Boolean);
      if (versions.length > 0) {
        config.blocked_versions = versions;
      } else {
        delete config.blocked_versions;
      }
    }

    setSaveError(undefined);
    mutation.mutate({
      request: {
        updateConfigurationRequestBody: { config },
      },
    });
  };

  return (
    <div className="border-border bg-card border p-6">
      <Stack gap={6}>
        <div>
          <Text variant="body" className="font-medium">
            Managed tools
          </Text>
          <Text muted small>
            Choose where the agent enforces each tool&apos;s configuration.
          </Text>
        </div>

        <div className="divide-border divide-y">
          {PLATFORMS.map((platform) => (
            <div
              key={platform.key}
              className="flex items-center justify-between gap-6 py-4 first:pt-0 last:pb-0"
            >
              <div>
                <Text variant="body" className="font-medium">
                  {platform.label}
                </Text>
                <Text muted small>
                  {platform.description}
                </Text>
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
          {isPlatformAdmin && (
            <Field>
              <FieldLabel htmlFor="device-agent-update-channel">
                Update channel
                <InternalAdminBadge />
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
          )}

          <Field>
            <FieldLabel htmlFor="device-agent-auto-update">
              Auto-update policy
            </FieldLabel>
            <Select
              value={autoUpdate}
              onValueChange={setAutoUpdate}
              disabled={disabled}
            >
              <SelectTrigger
                id="device-agent-auto-update"
                className="w-full"
                aria-label="Auto-update policy"
              >
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                <SelectItem
                  value={AUTO_UPDATE_INHERIT}
                  description="Use each device's local or MDM setting"
                >
                  Inherit
                </SelectItem>
                {AUTO_UPDATE_POLICIES.map((policy) => (
                  <SelectItem
                    key={policy.value}
                    value={policy.value}
                    description={policy.description}
                  >
                    {policy.label}
                  </SelectItem>
                ))}
                {isUnknownAutoUpdate && (
                  <SelectItem
                    value={autoUpdate}
                    description="Currently stored value not recognized by this UI"
                  >
                    {autoUpdate}
                  </SelectItem>
                )}
              </SelectContent>
            </Select>
            <FieldDescription>
              How enrolled agents handle new releases.
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

        {isPlatformAdmin && (
          <Field>
            <FieldLabel htmlFor="device-agent-blocked-versions">
              Blocked versions
              <InternalAdminBadge />
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
        )}

        <div className="bg-muted/40 border p-4">
          <Text muted small>
            After the first successful fetch, these settings override the same
            non-secret fields from local and MDM configuration. Device identity
            and credentials always remain local. If Gram is temporarily
            unreachable, agents use their last-known remote configuration; an
            agent without a cached remote configuration falls back to local
            settings.
          </Text>
        </div>

        {!configuration.isConfigured && (
          <Text muted small>
            No remote configuration is active yet. Saving enables this layer for
            enrolled agents.
          </Text>
        )}

        {saveError && (
          <ErrorAlert
            title="Unable to save device agent configuration"
            error={saveError}
            onDismiss={() => setSaveError(undefined)}
          />
        )}

        {/* Right-aligned at natural width — a Stack child would otherwise
            stretch the button across the full form. */}
        <div className="flex justify-end">
          <RequireScope
            scope="org:admin"
            level="component"
            reason="Organization admin access is required to change fleet configuration."
          >
            <Button
              variant="primary"
              onClick={handleSave}
              disabled={
                !isDirty || Boolean(intervalError) || mutation.isPending
              }
            >
              {mutation.isPending ? "Saving..." : "Save configuration"}
            </Button>
          </RequireScope>
        </div>
      </Stack>
    </div>
  );
}
